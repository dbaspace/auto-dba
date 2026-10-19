package ldapauth

import (
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/go-ldap/ldap/v3"

	"auto-dba/internal/store"
)

// UserInfo LDAP 校验成功后从目录取出的用户属性。
type UserInfo struct {
	DisplayName string
	Email       string
}

// 登录探测超时（search + user bind）；改密等管理操作仍用 defaultLDAPTimeout。
const loginLDAPTimeout = 5 * time.Second
const defaultLDAPTimeout = 15 * time.Second

func Verify(cfg *store.LdapConfig, username, password string) (*UserInfo, error) {
	endpoint, err := resolveEndpoint(cfg)
	if err != nil {
		return nil, err
	}
	if username == "" || password == "" {
		return nil, fmt.Errorf("username and password required")
	}
	start := time.Now()
	// 同一连接：Bind DN 搜用户后改为用户 Bind，避免第二次 dial
	conn, entry, lookupMS, err := openAndLookupUser(cfg, endpoint, username, loginLDAPTimeout)
	if err != nil {
		log.Printf("ldap verify username=%s lookup_err after %dms: %v", username, time.Since(start).Milliseconds(), err)
		return nil, err
	}
	defer conn.Close()
	info := &UserInfo{
		DisplayName: displayNameFromEntry(cfg, entry, username),
		Email:       emailFromEntry(entry),
	}
	bindStart := time.Now()
	if err := conn.Bind(entry.DN, password); err != nil {
		log.Printf("ldap verify username=%s lookup_ms=%d user_bind_ms=%d total_ms=%d bind_err",
			username, lookupMS, time.Since(bindStart).Milliseconds(), time.Since(start).Milliseconds())
		return nil, fmt.Errorf("invalid password")
	}
	log.Printf("ldap verify username=%s lookup_ms=%d user_bind_ms=%d total_ms=%d ok",
		username, lookupMS, time.Since(bindStart).Milliseconds(), time.Since(start).Milliseconds())
	return info, nil
}

// ChangePassword 仅修改 username 对应用户在目录中的密码（调用方须保证 username 为当前会话用户）。
func ChangePassword(cfg *store.LdapConfig, username, oldPassword, newPassword string) error {
	endpoint, err := resolveEndpoint(cfg)
	if err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if username == "" || oldPassword == "" || newPassword == "" {
		return fmt.Errorf("username and password required")
	}
	entry, err := lookupUserEntry(cfg, endpoint, username)
	if err != nil {
		return mapChangePasswordError(err)
	}
	userDN := entry.DN

	// 先验旧密
	if err := withUserBind(endpoint, userDN, oldPassword, func(*ldap.Conn) error { return nil }); err != nil {
		return fmt.Errorf("原密码错误")
	}

	type attempt struct {
		name string
		fn   func(conn *ldap.Conn) error
	}
	attempts := []attempt{
		{
			name: "PasswordModify(self)",
			fn: func(conn *ldap.Conn) error {
				_, e := conn.PasswordModify(ldap.NewPasswordModifyRequest("", oldPassword, newPassword))
				return e
			},
		},
		{
			name: "PasswordModify(dn)",
			fn: func(conn *ldap.Conn) error {
				_, e := conn.PasswordModify(ldap.NewPasswordModifyRequest(userDN, oldPassword, newPassword))
				return e
			},
		},
		{
			name: "Replace(userPassword)",
			fn: func(conn *ldap.Conn) error {
				mod := ldap.NewModifyRequest(userDN, nil)
				mod.Replace("userPassword", []string{newPassword})
				return conn.Modify(mod)
			},
		},
		{
			name: "DeleteAdd(userPassword)",
			fn: func(conn *ldap.Conn) error {
				mod := ldap.NewModifyRequest(userDN, nil)
				mod.Delete("userPassword", []string{oldPassword})
				mod.Add("userPassword", []string{newPassword})
				if e := conn.Modify(mod); e == nil {
					return nil
				}
				// 部分目录 Delete 不接受明文旧值，改为无值 Delete + Add
				mod2 := ldap.NewModifyRequest(userDN, nil)
				mod2.Delete("userPassword", nil)
				mod2.Add("userPassword", []string{newPassword})
				return conn.Modify(mod2)
			},
		},
	}
	if endpoint.useTLS {
		oldPwd := encodeADUnicodePwd(oldPassword)
		newPwd := encodeADUnicodePwd(newPassword)
		attempts = append(attempts,
			attempt{
				name: "DeleteAdd(unicodePwd)",
				fn: func(conn *ldap.Conn) error {
					mod := ldap.NewModifyRequest(userDN, nil)
					mod.Delete("unicodePwd", []string{oldPwd})
					mod.Add("unicodePwd", []string{newPwd})
					return conn.Modify(mod)
				},
			},
			attempt{
				name: "Replace(unicodePwd)",
				fn: func(conn *ldap.Conn) error {
					mod := ldap.NewModifyRequest(userDN, nil)
					mod.Replace("unicodePwd", []string{newPwd})
					return conn.Modify(mod)
				},
			},
		)
	}

	var lastErr error
	sawProtocolError := false
	for _, a := range attempts {
		err := withUserBind(endpoint, userDN, oldPassword, a.fn)
		if err == nil {
			return nil
		}
		lastErr = err
		if isLDAPProtocolError(err) {
			sawProtocolError = true
		}
	}
	if lastErr == nil {
		return fmt.Errorf("目录修改密码失败")
	}
	if !endpoint.useTLS && sawProtocolError {
		mapped := mapChangePasswordError(lastErr)
		if strings.Contains(mapped.Error(), "Protocol Error") || isLDAPProtocolError(lastErr) {
			return fmt.Errorf("目录不支持扩展改密；请在认证配置中启用 LDAPS/StartTLS 后重试，或联系管理员开放用户自助改密")
		}
		return mapped
	}
	return mapChangePasswordError(lastErr)
}

// ResetPassword 使用 Bind DN 重置用户目录密码（忘记密码场景，不校验旧密）。
func ResetPassword(cfg *store.LdapConfig, username, newPassword string) error {
	endpoint, err := resolveEndpoint(cfg)
	if err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	newPassword = strings.TrimSpace(newPassword)
	if username == "" || newPassword == "" {
		return fmt.Errorf("username and password required")
	}
	if strings.TrimSpace(cfg.BindDN) == "" || strings.TrimSpace(cfg.BindPassword) == "" {
		return fmt.Errorf("ldap bind password not set")
	}
	entry, err := lookupUserEntry(cfg, endpoint, username)
	if err != nil {
		return mapChangePasswordError(err)
	}
	userDN := entry.DN

	type attempt struct {
		name string
		fn   func(conn *ldap.Conn) error
	}
	attempts := []attempt{
		{
			name: "PasswordModify(admin)",
			fn: func(conn *ldap.Conn) error {
				_, e := conn.PasswordModify(ldap.NewPasswordModifyRequest(userDN, "", newPassword))
				return e
			},
		},
		{
			name: "Replace(userPassword)",
			fn: func(conn *ldap.Conn) error {
				mod := ldap.NewModifyRequest(userDN, nil)
				mod.Replace("userPassword", []string{newPassword})
				return conn.Modify(mod)
			},
		},
	}
	if endpoint.useTLS {
		newPwd := encodeADUnicodePwd(newPassword)
		attempts = append(attempts, attempt{
			name: "Replace(unicodePwd)",
			fn: func(conn *ldap.Conn) error {
				mod := ldap.NewModifyRequest(userDN, nil)
				mod.Replace("unicodePwd", []string{newPwd})
				return conn.Modify(mod)
			},
		})
	}

	var lastErr error
	for _, a := range attempts {
		err := withAdminBind(endpoint, cfg.BindDN, cfg.BindPassword, a.fn)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return fmt.Errorf("目录重置密码失败")
	}
	return mapChangePasswordError(lastErr)
}

func withAdminBind(endpoint *ldapEndpoint, bindDN, bindPassword string, fn func(*ldap.Conn) error) error {
	conn, err := dialLDAP(endpoint.addr, endpoint.host, endpoint.useTLS, endpoint.port)
	if err != nil {
		return fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	defer conn.Close()
	conn.SetTimeout(defaultLDAPTimeout)
	if err := conn.Bind(bindDN, bindPassword); err != nil {
		return fmt.Errorf("Bind DN 认证失败: %w", err)
	}
	if fn == nil {
		return nil
	}
	return fn(conn)
}

func withUserBind(endpoint *ldapEndpoint, userDN, password string, fn func(*ldap.Conn) error) error {
	conn, err := dialLDAP(endpoint.addr, endpoint.host, endpoint.useTLS, endpoint.port)
	if err != nil {
		return fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	defer conn.Close()
	conn.SetTimeout(defaultLDAPTimeout)
	if err := conn.Bind(userDN, password); err != nil {
		return err
	}
	if fn == nil {
		return nil
	}
	return fn(conn)
}

// encodeADUnicodePwd Active Directory unicodePwd：UTF-16LE，两端加双引号。
func encodeADUnicodePwd(password string) string {
	quoted := `"` + password + `"`
	u16 := utf16.Encode([]rune(quoted))
	buf := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	return string(buf)
}

func isLDAPProtocolError(err error) bool {
	if err == nil {
		return false
	}
	var le *ldap.Error
	if errors.As(err, &le) && le.ResultCode == ldap.LDAPResultProtocolError {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "protocol error") || strings.Contains(msg, "result code 2")
}

type ldapEndpoint struct {
	host   string
	port   int
	useTLS bool
	addr   string
}

func resolveEndpoint(cfg *store.LdapConfig) (*ldapEndpoint, error) {
	if cfg == nil || !cfg.Enabled || strings.TrimSpace(cfg.Host) == "" {
		return nil, fmt.Errorf("ldap not configured")
	}
	port := cfg.Port
	if port <= 0 {
		if cfg.UseTLS {
			port = 636
		} else {
			port = 389
		}
	}
	host := strings.TrimSpace(cfg.Host)
	useTLS := cfg.UseTLS
	for _, prefix := range []string{"ldaps://", "ldap://"} {
		if strings.HasPrefix(strings.ToLower(host), prefix) {
			if prefix == "ldaps://" {
				useTLS = true
			}
			host = strings.TrimSpace(host[len(prefix):])
			break
		}
	}
	if h, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		host = h
	}
	return &ldapEndpoint{
		host:   host,
		port:   port,
		useTLS: useTLS,
		addr:   net.JoinHostPort(host, fmt.Sprintf("%d", port)),
	}, nil
}

func lookupUserEntry(cfg *store.LdapConfig, endpoint *ldapEndpoint, username string) (*ldap.Entry, error) {
	conn, entry, _, err := openAndLookupUser(cfg, endpoint, username, defaultLDAPTimeout)
	if err != nil {
		return nil, err
	}
	conn.Close()
	return entry, nil
}

func openAndLookupUser(cfg *store.LdapConfig, endpoint *ldapEndpoint, username string, timeout time.Duration) (*ldap.Conn, *ldap.Entry, int64, error) {
	start := time.Now()
	conn, err := dialLDAP(endpoint.addr, endpoint.host, endpoint.useTLS, endpoint.port)
	dialMS := time.Since(start).Milliseconds()
	if err != nil {
		return nil, nil, dialMS, fmt.Errorf("连接 LDAP 失败: %w", err)
	}
	if timeout <= 0 {
		timeout = defaultLDAPTimeout
	}
	conn.SetTimeout(timeout)
	bindStart := time.Now()
	if strings.TrimSpace(cfg.BindDN) != "" {
		if strings.TrimSpace(cfg.BindPassword) == "" {
			conn.Close()
			return nil, nil, time.Since(start).Milliseconds(), fmt.Errorf("ldap bind password not set")
		}
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			conn.Close()
			return nil, nil, time.Since(start).Milliseconds(), fmt.Errorf("Bind DN 认证失败: %w", err)
		}
	}
	bindMS := time.Since(bindStart).Milliseconds()
	filter := BuildUserFilter(cfg.UserFilter, username)
	attrs := []string{"dn", "cn", "displayName", "name", "sn", "givenName", "sAMAccountName", "uid", "mail", "email", "userPrincipalName"}
	if a := strings.TrimSpace(cfg.DisplayNameAttr); a != "" {
		attrs = append(attrs, a)
	}
	searchStart := time.Now()
	result, err := searchUser(conn, cfg.BaseDN, filter, attrs)
	if err != nil {
		conn.Close()
		return nil, nil, time.Since(start).Milliseconds(), fmt.Errorf("搜索用户失败: %w", err)
	}
	if len(result.Entries) == 0 && !isBroadFilter(filter) {
		result, err = searchUser(conn, cfg.BaseDN, BroadUserFilter(username), attrs)
		if err != nil {
			conn.Close()
			return nil, nil, time.Since(start).Milliseconds(), fmt.Errorf("搜索用户失败: %w", err)
		}
	}
	searchMS := time.Since(searchStart).Milliseconds()
	if len(result.Entries) == 0 {
		conn.Close()
		return nil, nil, time.Since(start).Milliseconds(), fmt.Errorf("user not found")
	}
	if len(result.Entries) > 1 {
		conn.Close()
		return nil, nil, time.Since(start).Milliseconds(), fmt.Errorf("multiple users found")
	}
	total := time.Since(start).Milliseconds()
	log.Printf("ldap lookup username=%s dial_ms=%d bind_dn_ms=%d search_ms=%d total_ms=%d", username, dialMS, bindMS, searchMS, total)
	return conn, result.Entries[0], total, nil
}

// displayNameFromEntry 取中文/真实姓名：优先配置属性，再 displayName / name / 姓名组合 / cn。
// 若某属性值与登录名相同（常见 cn=账号），会跳过并继续找更像姓名的值。
func displayNameFromEntry(cfg *store.LdapConfig, entry *ldap.Entry, username string) string {
	if entry == nil {
		return ""
	}
	username = strings.TrimSpace(username)
	var candidates []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		for _, c := range candidates {
			if strings.EqualFold(c, v) {
				return
			}
		}
		candidates = append(candidates, v)
	}
	if cfg != nil {
		if a := strings.TrimSpace(cfg.DisplayNameAttr); a != "" {
			add(entry.GetAttributeValue(a))
		}
	}
	add(entry.GetAttributeValue("displayName"))
	add(entry.GetAttributeValue("name"))
	sn := strings.TrimSpace(entry.GetAttributeValue("sn"))
	given := strings.TrimSpace(entry.GetAttributeValue("givenName"))
	if looksLikeEmployeeID(sn) {
		add(given)
	} else if sn != "" || given != "" {
		add(sn + given) // 中文常见「姓+名」无空格
		if sn != "" && given != "" {
			add(given + " " + sn)
		}
	}
	add(entry.GetAttributeValue("cn"))

	return preferPersonName(candidates, username)
}

// employeePrefix 工号紧贴在汉字前：至少 4 位数字，或至少 6 位十六进制。
var employeePrefix = regexp.MustCompile(`^(?:[0-9]{4,}|[0-9a-fA-F]{6,})(\p{Han}.*)$`)

// CleanDisplayName 去掉紧贴在中文名前的工号，例如 1d449671赵培超 → 赵培超。
func CleanDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if m := employeePrefix.FindStringSubmatch(name); len(m) == 2 {
		return m[1]
	}
	return name
}

func looksLikeEmployeeID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	digits, hex := true, true
	for _, r := range s {
		if r < '0' || r > '9' {
			digits = false
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			hex = false
		}
	}
	if digits && len(s) >= 4 {
		return true
	}
	return hex && len(s) >= 6
}

func preferPersonName(candidates []string, username string) string {
	var usable []string
	for _, c := range candidates {
		if username != "" && strings.EqualFold(c, username) {
			continue
		}
		usable = append(usable, c)
	}
	if len(usable) == 0 {
		usable = candidates
	}
	for _, c := range usable {
		if CleanDisplayName(c) == c && !looksLikeEmployeeID(c) && personNameLike(c) {
			return c
		}
	}
	for _, c := range usable {
		if s := CleanDisplayName(c); s != "" && s != c {
			return s
		}
	}
	if len(usable) > 0 {
		return CleanDisplayName(usable[0])
	}
	return ""
}

func personNameLike(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func emailFromEntry(entry *ldap.Entry) string {
	if entry == nil {
		return ""
	}
	for _, attr := range []string{"mail", "email", "userPrincipalName"} {
		v := strings.TrimSpace(entry.GetAttributeValue(attr))
		if v == "" || !strings.Contains(v, "@") {
			continue
		}
		return v
	}
	return ""
}

func mapChangePasswordError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid password") || strings.Contains(msg, "原密码"):
		return fmt.Errorf("原密码错误")
	case strings.Contains(msg, "ldap not configured"):
		return fmt.Errorf("LDAP 未启用或未配置")
	case strings.Contains(msg, "bind password not set"):
		return fmt.Errorf("LDAP Bind 密码未配置")
	case strings.Contains(msg, "user not found"):
		return fmt.Errorf("目录中未找到当前用户")
	case strings.Contains(msg, "multiple users"):
		return fmt.Errorf("目录中匹配到多个用户，请检查用户过滤器")
	case isLDAPProtocolError(err):
		return fmt.Errorf("目录不支持 Password Modify 扩展，且属性改密失败；请启用 LDAPS 或联系管理员开放自助改密")
	case strings.Contains(msg, "confidentiality") || strings.Contains(msg, "stronger auth") ||
		(strings.Contains(msg, "tls") && !strings.Contains(msg, "protocol")) || strings.Contains(msg, "ssl"):
		return fmt.Errorf("目录要求加密连接（请启用 LDAPS/StartTLS）后再改密")
	case strings.Contains(msg, "insufficient") || strings.Contains(msg, "unwilling") ||
		strings.Contains(msg, "constraint") || strings.Contains(msg, "not allowed") ||
		strings.Contains(msg, "will not perform") || strings.Contains(msg, "access"):
		return fmt.Errorf("目录拒绝修改密码（权限或密码策略），请联系管理员")
	default:
		var le *ldap.Error
		if errors.As(err, &le) {
			return fmt.Errorf("目录修改密码失败: %s", le.Error())
		}
		return fmt.Errorf("目录修改密码失败: %v", err)
	}
}

func dialLDAP(addr, serverName string, useTLS bool, port int) (*ldap.Conn, error) {
	tlsConfig := &tls.Config{ServerName: serverName, InsecureSkipVerify: true}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	// ldaps 直连 / ldap + 可选 StartTLS
	if useTLS && port != 389 {
		return ldap.DialURL("ldaps://"+addr, ldap.DialWithTLSDialer(tlsConfig, dialer))
	}
	conn, err := ldap.DialURL("ldap://"+addr, ldap.DialWithDialer(dialer))
	if err != nil {
		return nil, err
	}
	if useTLS && port == 389 {
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

func searchUser(conn *ldap.Conn, baseDN, filter string, attrs []string) (*ldap.SearchResult, error) {
	req := ldap.NewSearchRequest(
		baseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0, 0, false,
		filter,
		attrs,
		nil,
	)
	return conn.Search(req)
}

func BroadUserFilter(username string) string {
	escaped := ldap.EscapeFilter(username)
	return "(|(uid=" + escaped + ")(cn=" + escaped + ")(sAMAccountName=" + escaped + ")(mail=" + escaped + ")(userPrincipalName=" + escaped + "))"
}

func isBroadFilter(filter string) bool {
	return strings.Contains(filter, "sAMAccountName=") || (strings.Contains(filter, "uid=") && strings.Contains(filter, "cn="))
}

func BuildUserFilter(userFilter, username string) string {
	escaped := ldap.EscapeFilter(username)
	raw := strings.TrimSpace(userFilter)
	if raw == "" {
		return BroadUserFilter(username)
	}
	filter := strings.ReplaceAll(raw, "%(user)s", escaped)
	filter = strings.ReplaceAll(filter, "%s", escaped)
	if filter == raw && !strings.Contains(raw, "%s") && !strings.Contains(raw, "%(user)s") {
		return BroadUserFilter(username)
	}
	return filter
}
