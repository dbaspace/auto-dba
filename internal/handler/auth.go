package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"auto-dba/internal/config"
	"auto-dba/internal/ldapauth"
	"auto-dba/internal/mail"
	"auto-dba/internal/middleware"
	"auto-dba/internal/ssoauth"
	"auto-dba/internal/store"
)

type AuthHandler struct {
	Store *store.DB
	Cfg   *config.Config
}

type loginReq struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	AuthMode    string `json:"auth_mode"`
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
	TenantID    string `json:"tenant_id"`
}

func ldapErrorMessage(err error) string {
	if err == nil {
		return "LDAP 认证失败"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "invalid password"):
		return "LDAP 密码错误"
	case strings.Contains(msg, "user not found"):
		return "LDAP 目录中未找到该用户，请检查用户名或用户搜索 Filter"
	case strings.Contains(msg, "multiple users found"):
		return "LDAP 匹配到多个用户，请收窄用户搜索 Filter"
	case strings.Contains(msg, "bind password not set"), strings.Contains(msg, "Bind 密码解密失败"):
		return "LDAP Bind 密码缺失或解密失败，请到「认证配置 → LDAP」重新保存 Bind 密码"
	case strings.Contains(msg, "ldap not configured"):
		return "LDAP 未启用或未配置"
	case strings.Contains(msg, "开发者角色"):
		return "开发者角色不存在，无法自动创建 LDAP 用户"
	default:
		return "LDAP 认证失败: " + msg
	}
}

func ssoErrorMessage(err error) string {
	if err == nil {
		return "SSO 认证失败"
	}
	msg := err.Error()
	switch {
	case ssoauth.IsUnsupportedGrant(err) || strings.Contains(msg, "unsupported_grant_type"):
		return "当前 SSO 不支持密码登录，请使用授权码方式登录"
	case strings.Contains(msg, "invalid password"):
		return "SSO 用户名或密码错误"
	case strings.Contains(msg, "user not found"):
		return "SSO 未找到该用户"
	case strings.Contains(msg, "Client Secret 解密失败"):
		return "SSO Client Secret 解密失败，请到「认证配置 → SSO」重新保存"
	case strings.Contains(msg, "sso not configured"), strings.Contains(msg, "base_url"), strings.Contains(msg, "client_id"):
		return "SSO 未启用或未配置完整"
	case strings.Contains(msg, "开发者角色"):
		return "开发者角色不存在，无法自动创建 SSO 用户"
	default:
		return "SSO 认证失败: " + msg
	}
}

// ssoAuthorizeGate 授权码跳转 IdP：只需 Enabled，Client Secret 解密失败不阻塞。
// 返回空字符串表示可继续。
func ssoAuthorizeGate(cfg *store.SsoConfig, err error) string {
	if cfg != nil && cfg.Enabled {
		return ""
	}
	if cfg == nil && err != nil {
		return ssoErrorMessage(err)
	}
	return "SSO 未启用"
}

// ssoExchangeGate 换票/回调：Enabled 且 Secret 可用（若曾加密保存）。解密失败返回明确提示，不伪装成「未启用」。
func ssoExchangeGate(cfg *store.SsoConfig, err error) string {
	if cfg == nil || !cfg.Enabled {
		if cfg == nil && err != nil {
			return ssoErrorMessage(err)
		}
		return "SSO 未启用"
	}
	if err != nil {
		return ssoErrorMessage(err)
	}
	// 有密文却解不出明文：与 GetSsoConfig 解密失败等价的兜底
	if strings.TrimSpace(cfg.ClientSecret) == "" && strings.TrimSpace(cfg.ClientSecretEnc) != "" {
		return "SSO Client Secret 解密失败，请到「认证配置 → SSO」重新保存"
	}
	return ""
}

func isExternalUserSource(source string) bool {
	return source == store.UserSourceLDAP || source == store.UserSourceSSO
}

// isCountableExternalAuthErr SSO/LDAP 实际发起校验后的失败均计入（含 unsupported_grant_type 等），
// 仅排除「未配置」类本地错误。
func isCountableExternalAuthErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not configured"),
		strings.Contains(msg, "base_url"),
		strings.Contains(msg, "client_id not set"),
		strings.Contains(msg, "bind password not set"),
		strings.Contains(msg, "开发者角色"):
		return false
	default:
		return true
	}
}

func (h *AuthHandler) writeAuthFail(c *gin.Context, username, userID, displayName, msg string, attempt *store.LoginAttempt) {
	body := gin.H{"message": msg}
	if attempt != nil {
		body["fail_count"] = attempt.FailCount
		body["require_captcha"] = attempt.RequireCaptcha() || attempt.InCooldown()
		if attempt.InCooldown() {
			body["retry_after_sec"] = attempt.RetryAfterSec()
			body["locked"] = true
		}
	}
	h.recordLogin(c, username, userID, displayName, false, msg)
	c.JSON(http.StatusUnauthorized, body)
}

func (h *AuthHandler) failExternal(c *gin.Context, username, userID, displayName, msg string, countFailure bool) {
	var attempt *store.LoginAttempt
	if countFailure {
		a, err := h.Store.BumpExternalLoginFailure(username)
		if err != nil {
			log.Printf("bump external login failure username=%s err=%v", username, err)
		} else {
			attempt = a
			if a != nil && a.InCooldown() {
				msg = a.CooldownMessage()
			}
		}
	} else {
		attempt, _ = h.Store.EnsureLoginAttemptReady(username)
	}
	h.writeAuthFail(c, username, userID, displayName, msg, attempt)
}

// guardExternalAttempt 外部认证前：冷却检查 + 失败≥3 次校验验证码。
func (h *AuthHandler) guardExternalAttempt(c *gin.Context, req *loginReq) bool {
	attempt, err := h.Store.EnsureLoginAttemptReady(req.Username)
	if err != nil {
		log.Printf("login attempt ready username=%s err=%v", req.Username, err)
		return true
	}
	if attempt.InCooldown() {
		h.writeAuthFail(c, req.Username, "", "", attempt.CooldownMessage(), attempt)
		return false
	}
	if attempt.RequireCaptcha() {
		if !verifyCaptcha(req.CaptchaID, req.CaptchaCode) {
			a, _ := h.Store.BumpExternalLoginFailure(req.Username)
			msg := "验证码错误或已过期"
			if a != nil && a.InCooldown() {
				msg = a.CooldownMessage()
			}
			h.writeAuthFail(c, req.Username, "", "", msg, a)
			return false
		}
	}
	return true
}

func (h *AuthHandler) Login(c *gin.Context) {
	start := time.Now()
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "username and password required"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.AuthMode = strings.ToLower(strings.TrimSpace(req.AuthMode))
	autoTry := req.AuthMode == "" || req.AuthMode == "auto"
	forceSSO := req.AuthMode == "sso"
	forceLDAP := req.AuthMode == "ldap"
	forcePlatform := req.AuthMode == "platform"
	if !forceLDAP {
		req.Password = strings.TrimSpace(req.Password)
	}
	isAdmin := strings.EqualFold(req.Username, "admin")
	if (forceSSO || forceLDAP) && isAdmin {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "admin 请使用平台账号登录"})
		return
	}

	exist, _ := h.Store.GetUserByUsername(req.Username)
	if exist != nil && exist.IsLoginBlocked() {
		msg := exist.LoginBlockedMessage()
		h.recordLogin(c, req.Username, exist.ID, exist.DisplayName, false, msg)
		c.JSON(http.StatusUnauthorized, gin.H{"message": msg})
		return
	}
	// 已有本地用户：按 source 定向，避免 SSO 抢先失败后吞掉 LDAP 错误
	if exist != nil && !isAdmin && autoTry && !forceSSO && !forceLDAP && !forcePlatform {
		switch exist.Source {
		case store.UserSourceLDAP:
			forceLDAP = true
			autoTry = false
		case store.UserSourceSSO:
			forceSSO = true
			autoTry = false
		case store.UserSourcePlatform, "":
			forcePlatform = true
			autoTry = false
		}
	}

	// 平台账号/admin 不读 SSO/LDAP 配置（避免两次全局参数 + 解密）
	var ssoCfg *store.SsoConfig
	var ldapCfg *store.LdapConfig
	var ssoCfgErr, ldapCfgErr error
	needSSO := !isAdmin && !forcePlatform && !forceLDAP && (autoTry || forceSSO)
	needLDAP := !isAdmin && !forcePlatform && (autoTry || forceLDAP)
	if needSSO || forceSSO {
		ssoCfg, ssoCfgErr = h.Store.GetSsoConfig(h.encKey())
	}
	if needLDAP || forceLDAP {
		ldapCfg, ldapCfgErr = h.Store.GetLdapConfig(h.encKey())
	}
	ssoOn := ssoCfg != nil && ssoCfg.Enabled
	ssoPassword := ssoOn && ssoCfg.UsePasswordGrant()
	ldapOn := ldapCfg != nil && ldapCfg.Enabled

	willExternal := !isAdmin && !forcePlatform && ((ssoPassword && (autoTry || forceSSO)) || (ldapOn && (autoTry || forceLDAP)))
	if willExternal {
		if !h.guardExternalAttempt(c, &req) {
			return
		}
	}

	if forceSSO && !ssoOn {
		msg := "SSO 未启用"
		if ssoCfgErr != nil {
			msg = ssoErrorMessage(ssoCfgErr)
		}
		h.recordLogin(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), false, msg)
		c.JSON(http.StatusUnauthorized, gin.H{"message": msg})
		return
	}
	if forceSSO && ssoOn && !ssoPassword {
		h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), "当前 SSO 为授权码模式，请使用「登录」跳转统一认证", false)
		return
	}
	if forceLDAP && !ldapOn {
		msg := "LDAP 未启用"
		if ldapCfgErr != nil {
			msg = ldapErrorMessage(ldapCfgErr)
		}
		if exist != nil && exist.Source == store.UserSourceLDAP {
			msg = "LDAP 未启用，无法登录该目录账号"
		}
		h.recordLogin(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), false, msg)
		c.JSON(http.StatusUnauthorized, gin.H{"message": msg})
		return
	}

	failAuth := func(username, userID, displayName, msg string, countFailure bool) {
		if countFailure && userID != "" {
			if locked, err := h.Store.RecordLoginFailure(userID); err == nil && locked {
				msg = "账号已锁定，请联系管理员解锁"
			}
		}
		h.recordLogin(c, username, userID, displayName, false, msg)
		c.JSON(http.StatusUnauthorized, gin.H{"message": msg})
	}

	// 外部链路实际校验失败（不含「仅授权码」跳过）；最终失败时按外部规则计数
	externalFailed := false
	var externalMS int64

	// 1) SSO（密码模式；IdP 若仅支持授权码会返回 unsupported_grant_type）
	if !isAdmin && !forceLDAP && !forcePlatform && ssoPassword && (autoTry || forceSSO) {
		if ssoCfgErr != nil {
			msg := ssoErrorMessage(ssoCfgErr)
			if forceSSO || !autoTry {
				h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), msg, false)
				return
			}
			log.Printf("login sso config username=%s reason=%v", req.Username, ssoCfgErr)
		} else {
			extStart := time.Now()
			u, created, serr := h.trySSOLogin(ssoCfg, req.Username, req.Password)
			externalMS += time.Since(extStart).Milliseconds()
			if serr == nil && u != nil {
				msg := "SSO 登录成功"
				if created {
					msg = "SSO 登录成功（已自动创建本地账号）"
				}
				h.respondLoginWithTenant(c, u, msg, req.TenantID)
				log.Printf("login timing username=%s mode=sso external_ms=%d total_ms=%d", req.Username, externalMS, time.Since(start).Milliseconds())
				return
			}
			if serr != nil {
				log.Printf("login sso username=%s reason=%v", req.Username, serr)
				msg := ssoErrorMessage(serr)
				if ssoauth.IsUnsupportedGrant(serr) {
					// forceSSO：计入失败，防止无限打 token 端点
					if forceSSO || !autoTry {
						h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), msg, true)
						return
					}
					// auto：改走 LDAP/平台；若后续仍失败则按 willExternal 计数
				} else {
					count := isCountableExternalAuthErr(serr)
					if count {
						externalFailed = true
					}
					if forceSSO || !autoTry {
						h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), msg, count)
						return
					}
					// auto：凭证类错误不再双探 LDAP，避免叠加超时
					if count {
						h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), msg, true)
						return
					}
				}
			}
		}
	}
	if forceSSO {
		h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), "SSO 认证失败，请使用「SSO 登录」按钮（授权码）", externalFailed)
		return
	}

	// 2) LDAP
	if !isAdmin && !forcePlatform && ldapOn && (autoTry || forceLDAP) {
		if ldapCfgErr != nil {
			msg := ldapErrorMessage(ldapCfgErr)
			if forceLDAP || !autoTry {
				h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), msg, false)
				return
			}
			log.Printf("login ldap config username=%s reason=%v", req.Username, ldapCfgErr)
		} else {
			extStart := time.Now()
			u, created, lerr := h.tryLDAPLogin(ldapCfg, req.Username, req.Password)
			externalMS += time.Since(extStart).Milliseconds()
			if lerr == nil && u != nil {
				msg := "LDAP 登录成功"
				if created {
					msg = "LDAP 登录成功（已自动创建本地账号）"
				}
				h.respondLoginWithTenant(c, u, msg, req.TenantID)
				log.Printf("login timing username=%s mode=ldap external_ms=%d total_ms=%d", req.Username, externalMS, time.Since(start).Milliseconds())
				return
			}
			if lerr != nil {
				log.Printf("login ldap username=%s reason=%v", req.Username, lerr)
				msg := ldapErrorMessage(lerr)
				count := isCountableExternalAuthErr(lerr)
				if count {
					externalFailed = true
				}
				if forceLDAP || !autoTry {
					h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), msg, count)
					return
				}
			}
		}
	}
	if forceLDAP {
		h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), "LDAP 认证失败", externalFailed)
		return
	}

	// auto：外部凭据已失败 → 计入并结束（避免无计数落到平台无限重试）
	if externalFailed && autoTry && !forcePlatform {
		h.failExternal(c, req.Username, idOrEmpty(exist), nameOrEmpty(exist), "用户名或密码错误", true)
		return
	}

	// 3) 平台本地（含 SSO 仅支持授权码时的回退）
	authStart := time.Now()
	u, err := h.Store.GetUserByUsername(req.Username)
	if err != nil || u == nil {
		if willExternal {
			h.failExternal(c, req.Username, "", "", "用户名或密码错误", true)
			return
		}
		failAuth(req.Username, "", "", "用户名或密码错误", false)
		return
	}
	if u.IsLoginBlocked() {
		failAuth(u.Username, u.ID, u.DisplayName, u.LoginBlockedMessage(), false)
		return
	}
	if isExternalUserSource(u.Source) {
		msg := "外部认证用户请使用目录/SSO 密码登录"
		if u.Source == store.UserSourceLDAP {
			msg = "LDAP 用户请使用目录密码登录"
			if !ldapOn {
				msg = "LDAP 未启用，无法登录该目录账号"
			}
		} else if u.Source == store.UserSourceSSO {
			msg = "SSO 用户请使用 SSO 密码登录"
			if !ssoOn {
				msg = "SSO 未启用，无法登录该 SSO 账号"
			}
		}
		h.failExternal(c, u.Username, u.ID, u.DisplayName, msg, true)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		// 走过外部认证链路的失败统一走外部计数（验证码/冷却）
		if willExternal {
			h.failExternal(c, u.Username, u.ID, u.DisplayName, "用户名或密码错误", true)
			return
		}
		failAuth(u.Username, u.ID, u.DisplayName, "用户名或密码错误", true)
		return
	}
	authMS := time.Since(authStart).Milliseconds()
	h.respondLoginWithTenant(c, u, "登录成功", req.TenantID)
	log.Printf("login timing username=%s mode=platform auth_ms=%d external_ms=%d total_ms=%d", req.Username, authMS, externalMS, time.Since(start).Milliseconds())
}

func idOrEmpty(u *store.User) string {
	if u == nil {
		return ""
	}
	return u.ID
}

func nameOrEmpty(u *store.User) string {
	if u == nil {
		return ""
	}
	return u.DisplayName
}

func (h *AuthHandler) encKey() string {
	if h == nil || h.Cfg == nil {
		return ""
	}
	return h.Cfg.FieldCrypto()
}

func (h *AuthHandler) tryLDAPLogin(cfg *store.LdapConfig, username, password string) (*store.User, bool, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, false, errors.New("ldap not configured")
	}
	if strings.TrimSpace(cfg.BindDN) != "" && strings.TrimSpace(cfg.BindPassword) == "" {
		return nil, false, errors.New("ldap bind password not set")
	}
	info, err := ldapauth.Verify(cfg, username, password)
	if err != nil {
		return nil, false, err
	}
	if info == nil {
		info = &ldapauth.UserInfo{}
	}
	// 本地用户名 = 登录名
	u, err := h.Store.GetUserByUsername(username)
	if err != nil {
		return nil, false, err
	}
	created := false
	displayName := strings.TrimSpace(info.DisplayName)
	if displayName == "" {
		displayName = username
	}
	email := strings.TrimSpace(info.Email)
	if u == nil {
		role, err := h.Store.GetRoleByCode("developer")
		if err != nil || role == nil {
			return nil, false, errors.New("开发者角色不存在，无法自动创建 LDAP 用户")
		}
		u = &store.User{
			Username:    username,
			DisplayName: displayName,
			Email:       email,
			Role:        role.Code,
			RoleID:      role.ID,
			Source:      store.UserSourceLDAP,
		}
		if err := h.Store.CreateUser(u, store.LDAPPlaceholderHash); err != nil {
			return nil, false, err
		}
		created = true
	} else {
		if displayName != "" && displayName != u.DisplayName {
			_ = h.Store.UpdateUserDisplayName(u.ID, displayName)
			u.DisplayName = displayName
		}
		if email != "" {
			_ = h.Store.SetUserEmail(u.ID, email)
			u.Email = email
		}
	}
	return u, created, nil
}

func (h *AuthHandler) trySSOLogin(cfg *store.SsoConfig, username, password string) (*store.User, bool, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, false, errors.New("sso not configured")
	}
	if strings.TrimSpace(cfg.ClientID) == "" || cfg.TokenURL() == "" {
		return nil, false, errors.New("sso base_url / client_id not set")
	}
	info, err := ssoauth.Verify(cfg, username, password)
	if err != nil {
		return nil, false, err
	}
	return h.upsertSSOUser(info, username)
}

func (h *AuthHandler) upsertSSOUser(info *ssoauth.UserInfo, fallbackUsername string) (*store.User, bool, error) {
	localName := strings.TrimSpace(fallbackUsername)
	if info != nil && strings.TrimSpace(info.Username) != "" {
		localName = strings.TrimSpace(info.Username)
	}
	if localName == "" {
		return nil, false, errors.New("sso userinfo missing username")
	}
	u, err := h.Store.GetUserByUsername(localName)
	if err != nil {
		return nil, false, err
	}
	if u == nil && fallbackUsername != "" && !strings.EqualFold(localName, fallbackUsername) {
		u, err = h.Store.GetUserByUsername(fallbackUsername)
		if err != nil {
			return nil, false, err
		}
	}
	created := false
	if u == nil {
		role, err := h.Store.GetRoleByCode("developer")
		if err != nil || role == nil {
			return nil, false, errors.New("开发者角色不存在，无法自动创建 SSO 用户")
		}
		displayName := localName
		email := ""
		if info != nil {
			if strings.TrimSpace(info.DisplayName) != "" {
				displayName = strings.TrimSpace(info.DisplayName)
			}
			email = strings.TrimSpace(info.Email)
		}
		u = &store.User{
			Username:    localName,
			DisplayName: displayName,
			Email:       email,
			Role:        role.Code,
			RoleID:      role.ID,
			Source:      store.UserSourceSSO,
		}
		if err := h.Store.CreateUser(u, store.LDAPPlaceholderHash); err != nil {
			return nil, false, err
		}
		created = true
	}
	return u, created, nil
}

// resolveSSOCallbackURL 解析 authorize/换票用的 redirect_uri。
// 优先配置 CallbackURL；为空时用 frontendOrigin+/auth/callback；再无则回退后端回调路径。
func (h *AuthHandler) resolveSSOCallbackURL(c *gin.Context, cfg *store.SsoConfig, frontendOrigin string) string {
	if cfg != nil {
		if cb := strings.TrimSpace(cfg.CallbackURL); cb != "" {
			return cb
		}
	}
	frontendOrigin = strings.TrimSpace(frontendOrigin)
	if frontendOrigin != "" {
		return strings.TrimRight(frontendOrigin, "/") + "/auth/callback"
	}
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host
	if fwd := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host + "/api/v1/auth/sso/callback"
}

// sameLoopbackRedirectURI 比较两个 redirect_uri；localhost / 127.0.0.1 / ::1 视为等价。
func sameLoopbackRedirectURI(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return a == b
	}
	if a == b {
		return true
	}
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	if ua.Scheme != ub.Scheme || ua.Path != ub.Path || ua.RawQuery != ub.RawQuery {
		return false
	}
	ha, pa := splitHostPortLoose(ua.Host)
	hb, pb := splitHostPortLoose(ub.Host)
	if pa != pb {
		return false
	}
	return normalizeLoopbackHost(ha) == normalizeLoopbackHost(hb)
}

func splitHostPortLoose(hostport string) (host, port string) {
	hostport = strings.TrimSpace(hostport)
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		return h, p
	}
	return hostport, ""
}

func normalizeLoopbackHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	switch h {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return "loopback"
	default:
		return h
	}
}

func (h *AuthHandler) SSOStart(c *gin.Context) {
	cfg, err := h.Store.GetSsoConfig(h.encKey())
	if msg := ssoAuthorizeGate(cfg, err); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	if !cfg.UseAuthorizeGrant() {
		c.JSON(http.StatusBadRequest, gin.H{"message": "当前 SSO 为密码模式，请使用账号密码登录"})
		return
	}
	frontend := strings.TrimSpace(c.Query("frontend"))
	if frontend == "" {
		if ref := strings.TrimSpace(c.Request.Referer()); ref != "" {
			if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
				frontend = u.Scheme + "://" + u.Host
			}
		}
	}
	if frontend == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "缺少 frontend 参数（前端页面 Origin）"})
		return
	}
	redirectPath := strings.TrimSpace(c.Query("redirect"))
	callbackURL := h.resolveSSOCallbackURL(c, cfg, frontend)
	if callbackURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "SSO 回调地址未配置，请在认证配置中填写与 IdP 白名单一致的 callback_url"})
		return
	}
	log.Printf("sso authorize redirect_uri=%s frontend=%s", callbackURL, frontend)
	state := ssoauth.PutState(frontend, redirectPath, callbackURL)
	authURL, err := ssoauth.BuildAuthorizeURL(cfg, state, callbackURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": ssoErrorMessage(err)})
		return
	}
	c.Redirect(http.StatusFound, authURL)
}

// finishSSOCodeExchange 用授权码换本地 ticket。优先使用 authorize 时写入 state 的 redirect_uri。
func (h *AuthHandler) finishSSOCodeExchange(c *gin.Context, cfg *store.SsoConfig, code, state, expectedRedirectURI string) (ticket, frontend, redirectPath string, errMsg string, httpStatus int) {
	frontend, redirectPath, storedCallback, ok := ssoauth.TakeState(state)
	if !ok {
		return "", "", "", "SSO state 无效或已过期（若刚重启过后端请重新从登录页发起）", http.StatusBadRequest
	}
	if code == "" {
		return "", "", "", "缺少授权码 code", http.StatusBadRequest
	}
	redirectURI := strings.TrimSpace(storedCallback)
	if redirectURI == "" {
		redirectURI = strings.TrimSpace(expectedRedirectURI)
	}
	if redirectURI == "" {
		redirectURI = h.resolveSSOCallbackURL(c, cfg, frontend)
	}
	info, err := ssoauth.ExchangeCode(cfg, code, redirectURI)
	if err != nil {
		log.Printf("sso code exchange: %v", err)
		h.recordLogin(c, "", "", "", false, "SSO 换票失败: "+err.Error())
		return "", "", "", "SSO 换票失败: " + ssoErrorMessage(err), http.StatusBadRequest
	}
	u, created, err := h.upsertSSOUser(info, "")
	if err != nil {
		return "", "", "", ssoErrorMessage(err), http.StatusBadRequest
	}
	if u.IsLoginBlocked() {
		return "", "", "", u.LoginBlockedMessage(), http.StatusForbidden
	}
	_ = h.Store.ClearLoginFailures(u.ID)
	_, _ = h.Store.ClearExternalLoginFailure(u.Username)
	ttl := time.Duration(h.Cfg.App.JWTTTLHours) * time.Hour
	tid, tcode := h.resolveLoginTenantIDs(u.ID, "")
	tok, err := middleware.SignToken(h.Cfg.App.JWTSecret, u.ID, u.Username, u.Role, tid, tcode, ttl)
	if err != nil {
		return "", "", "", err.Error(), http.StatusInternalServerError
	}
	msg := "SSO 授权码登录成功"
	if created {
		msg = "SSO 授权码登录成功（已自动创建本地账号）"
	}
	h.recordLogin(c, u.Username, u.ID, u.DisplayName, true, msg)
	middleware.TouchIdle(u.ID)
	out := h.userPublicJSON(u, nil)
	out["menus"] = []store.MenuNode{}
	out["permissions"] = []string{}
	out["platform_permissions"] = []string{}
	if tid != "" {
		if t, _ := h.Store.GetTenant(tid); t != nil {
			out["current_tenant"] = t
		}
	}
	log.Printf("login timing username=%s mode=sso_code menus_ms=0(deferred)", u.Username)
	userBlob, _ := json.Marshal(out)
	ticket = ssoauth.PutTicket(tok, userBlob)
	return ticket, frontend, redirectPath, "", http.StatusOK
}

func (h *AuthHandler) SSOCallback(c *gin.Context) {
	cfg, err := h.Store.GetSsoConfig(h.encKey())
	if msg := ssoExchangeGate(cfg, err); msg != "" {
		c.String(http.StatusBadRequest, "%s", msg)
		return
	}
	if errMsg := strings.TrimSpace(c.Query("error")); errMsg != "" {
		desc := strings.TrimSpace(c.Query("error_description"))
		msg := errMsg
		if desc != "" {
			msg = errMsg + ": " + desc
		}
		h.recordLogin(c, "", "", "", false, "SSO 回调失败: "+msg)
		c.String(http.StatusBadRequest, "SSO 授权失败: %s", msg)
		return
	}
	code := strings.TrimSpace(c.Query("code"))
	state := strings.TrimSpace(c.Query("state"))
	callbackURL := h.resolveSSOCallbackURL(c, cfg, "")
	ticket, frontend, redirectPath, errMsg, status := h.finishSSOCodeExchange(c, cfg, code, state, callbackURL)
	if errMsg != "" {
		c.String(status, "%s", errMsg)
		return
	}
	q := url.Values{}
	q.Set("sso_ticket", ticket)
	if redirectPath != "" {
		q.Set("redirect", redirectPath)
	}
	dest := strings.TrimRight(frontend, "/") + "/#/login?" + q.Encode()
	c.Redirect(http.StatusFound, dest)
}

type ssoExchangeReq struct {
	Code        string `json:"code"`
	State       string `json:"state"`
	RedirectURI string `json:"redirect_uri"`
}

// SSOExchange 供前端回调页用：IdP 把 code 打到前端后，再 POST 到此换 ticket。
func (h *AuthHandler) SSOExchange(c *gin.Context) {
	cfg, err := h.Store.GetSsoConfig(h.encKey())
	if msg := ssoExchangeGate(cfg, err); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": msg})
		return
	}
	if !cfg.UseAuthorizeGrant() {
		c.JSON(http.StatusBadRequest, gin.H{"message": "当前 SSO 为密码模式，请使用账号密码登录"})
		return
	}
	var req ssoExchangeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	code := strings.TrimSpace(req.Code)
	state := strings.TrimSpace(req.State)
	redirectURI := strings.TrimSpace(req.RedirectURI)
	expected := h.resolveSSOCallbackURL(c, cfg, "")
	if redirectURI == "" {
		redirectURI = expected
	} else if !sameLoopbackRedirectURI(redirectURI, expected) {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": fmt.Sprintf("redirect_uri 与配置的 SSO 回调地址不一致（收到 %s，配置 %s）。请统一使用 127.0.0.1 或 localhost 打开页面，并与认证配置中的回调一致", redirectURI, expected),
		})
		return
	}
	// 换票时以 authorize 写入 state 的 URI 为准（expected 仅作校验）
	ticket, _, redirectPath, errMsg, status := h.finishSSOCodeExchange(c, cfg, code, state, expected)
	if errMsg != "" {
		c.JSON(status, gin.H{"message": errMsg})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ticket":   ticket,
		"redirect": redirectPath,
	})
}

func (h *AuthHandler) SSOTicket(c *gin.Context) {
	tok, userJSON, ok := ssoauth.TakeTicket(c.Query("ticket"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "SSO ticket 无效或已过期"})
		return
	}
	var user any
	_ = json.Unmarshal(userJSON, &user)
	c.JSON(http.StatusOK, gin.H{"token": tok, "user": user})
}

func (h *AuthHandler) resolveLoginTenantIDs(userID, preferred string) (tid, tcode string) {
	t, list, err := h.Store.PickLoginTenant(userID, preferred)
	if err != nil || t == nil {
		if len(list) == 1 {
			return list[0].ID, list[0].Code
		}
		return "", ""
	}
	return t.ID, t.Code
}

func (h *AuthHandler) respondLogin(c *gin.Context, u *store.User, logMsg string) {
	h.respondLoginWithTenant(c, u, logMsg, "")
}

func (h *AuthHandler) respondLoginWithTenant(c *gin.Context, u *store.User, logMsg, preferredTenant string) {
	// 成功路径异步清失败计数，不挡响应
	uid, uname := u.ID, u.Username
	storeDB := h.Store
	go func() {
		defer func() { _ = recover() }()
		if storeDB == nil {
			return
		}
		_ = storeDB.ClearLoginFailures(uid)
		_, _ = storeDB.ClearExternalLoginFailure(uname)
	}()
	preferred := strings.TrimSpace(preferredTenant)
	if preferred == "" {
		preferred = strings.TrimSpace(c.Query("tenant_id"))
	}
	// 自愈：历史「加入其它租户即踢出默认租户」曾导致 admin 顶栏只剩业务租户名称
	_ = h.Store.EnsurePlatformAdminDefaultMembership(u.ID)
	tenantStart := time.Now()
	t, tenants, err := h.Store.PickLoginTenant(u.ID, preferred)
	tenantMS := time.Since(tenantStart).Milliseconds()
	ttl := time.Duration(h.Cfg.App.JWTTTLHours) * time.Hour
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"message": err.Error(), "tenants": tenants})
		return
	}
	if t == nil && len(tenants) > 1 {
		tok, err := middleware.SignToken(h.Cfg.App.JWTSecret, u.ID, u.Username, u.Role, "", "", ttl)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		h.recordLogin(c, u.Username, u.ID, u.DisplayName, true, logMsg+"（待选租户）")
		middleware.TouchIdle(u.ID)
		c.JSON(http.StatusOK, gin.H{
			"token":              tok,
			"need_tenant_select": true,
			"tenants":            tenants,
			"user":               userPublicJSON(u, nil),
		})
		return
	}
	tid, tcode := "", ""
	if t != nil {
		tid, tcode = t.ID, t.Code
	} else if len(tenants) == 1 {
		tid, tcode = tenants[0].ID, tenants[0].Code
		t = &tenants[0]
	}
	tok, err := middleware.SignToken(h.Cfg.App.JWTSecret, u.ID, u.Username, u.Role, tid, tcode, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	h.recordLogin(c, u.Username, u.ID, u.DisplayName, true, logMsg)
	middleware.TouchIdle(u.ID)
	// 登录响应不装配菜单/权限（省数百 ms～1s）；前端进布局后 /auth/me 拉取。
	// 避免 LDAP 已耗数秒时再叠加 RBAC，按钮转圈明显变长。
	out := h.userPublicJSON(u, nil)
	out["current_tenant"] = t
	out["tenants"] = tenants
	out["menus"] = []store.MenuNode{}
	out["permissions"] = []string{}
	out["platform_permissions"] = []string{}
	log.Printf("login timing username=%s tenant_ms=%d menus_ms=0(deferred)", u.Username, tenantMS)
	c.JSON(http.StatusOK, gin.H{
		"token":         tok,
		"user":          out,
		"menus_pending": true,
	})
}

// applyUserAccess 一次 ListMenus 写入 menus / permissions / platform_permissions。
func (h *AuthHandler) applyUserAccess(out gin.H, u *store.User, tenantID string, impersonate bool) {
	if out == nil || h == nil || h.Store == nil || u == nil {
		return
	}
	acc, err := h.Store.BootstrapUserAccessFromUser(u, u.ID, tenantID, true, impersonate)
	if err != nil || acc == nil {
		if err != nil {
			log.Printf("bootstrap user access user=%s tenant=%s: %v", u.ID, tenantID, err)
		}
		return
	}
	out["menus"] = acc.Menus
	out["permissions"] = acc.Permissions
	out["platform_permissions"] = acc.PlatformPermissions
}

func (h *AuthHandler) Info(c *gin.Context) {
	modes := []string{"platform"}
	priority := []string{}
	ssoGrantMode := ""
	ssoAuthorize := false
	ssoCallbackURL := ""
	if cfg, _ := h.Store.GetSsoConfig(h.encKey()); cfg != nil && cfg.Enabled {
		modes = append(modes, "sso")
		priority = append(priority, "sso")
		ssoGrantMode = store.NormalizeSSOGrantMode(cfg.GrantMode)
		ssoAuthorize = cfg.UseAuthorizeGrant() && strings.TrimSpace(cfg.ClientID) != "" && cfg.AuthorizeURL() != ""
		ssoCallbackURL = strings.TrimSpace(cfg.CallbackURL)
	}
	if cfg, _ := h.Store.GetLdapConfig(h.encKey()); cfg != nil && cfg.Enabled {
		modes = append(modes, "ldap")
		priority = append(priority, "ldap")
	}
	priority = append(priority, "platform")
	c.JSON(http.StatusOK, gin.H{
		"auth_modes":       modes,
		"auth_priority":    priority,
		"auth_auto":        true,
		"sso_grant_mode":   ssoGrantMode,
		"sso_authorize":    ssoAuthorize,
		"sso_login_path":   "/api/v1/auth/sso/login",
		"sso_callback_url": ssoCallbackURL,
	})
}

func (h *AuthHandler) recordLogin(c *gin.Context, username, userID, displayName string, ok bool, msg string) {
	if h == nil || h.Store == nil {
		return
	}
	ip := clientLoginIP(c)
	ua := ""
	if c != nil && c.Request != nil {
		ua = c.Request.UserAgent()
	}
	storeDB := h.Store
	go func() {
		defer func() { _ = recover() }()
		if storeDB == nil {
			return
		}
		if err := storeDB.CreateLoginLog(&store.LoginLog{
			UserID:      userID,
			Username:    username,
			DisplayName: displayName,
			Success:     ok,
			Message:     msg,
			IP:          ip,
			UserAgent:   ua,
		}); err != nil {
			log.Printf("login log: %v", err)
		}
	}()
}

func clientLoginIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return localNonLoopbackIP()
	}
	var loopback string
	for _, raw := range []string{
		c.GetHeader("X-Forwarded-For"),
		c.GetHeader("X-Real-IP"),
		c.GetHeader("X-Client-IP"),
		forwardedFor(c.GetHeader("Forwarded")),
		c.ClientIP(),
		c.Request.RemoteAddr,
	} {
		for _, part := range splitIPList(raw) {
			ip := stripPort(part)
			if ip == "" || !looksLikeIP(ip) {
				continue
			}
			if isLoopbackIP(ip) {
				if loopback == "" {
					loopback = ip
				}
				continue
			}
			return ip
		}
	}
	if lan := localNonLoopbackIP(); lan != "" {
		return lan
	}
	return loopback
}

func firstForwardedIP(raw string) string {
	for _, p := range splitIPList(raw) {
		ip := stripPort(p)
		if ip == "" || isLoopbackIP(ip) {
			continue
		}
		return ip
	}
	return ""
}

func splitIPList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func forwardedFor(raw string) string {
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if len(part) < 5 || !strings.EqualFold(part[:4], "for=") {
			continue
		}
		return strings.Trim(part[4:], `"`)
	}
	return ""
}

func looksLikeIP(s string) bool {
	return net.ParseIP(s) != nil
}

func isLoopbackIP(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.IsLoopback()
}

func localNonLoopbackIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	var fallback string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP == nil || ipn.IP.IsLoopback() {
			continue
		}
		ip := ipn.IP
		if v4 := ip.To4(); v4 != nil {
			if v4.IsPrivate() {
				return v4.String()
			}
			if fallback == "" {
				fallback = v4.String()
			}
			continue
		}
		if fallback == "" && ip.IsPrivate() {
			fallback = ip.String()
		}
	}
	return fallback
}

func stripPort(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return strings.Trim(addr, "[]")
}

func (h *AuthHandler) ListLoginLogs(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可查看登录日志") {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	f := store.LoginLogFilter{Username: c.Query("username"), Page: page, PageSize: pageSize}
	if raw := strings.TrimSpace(c.Query("success")); raw != "" {
		ok := raw == "1" || strings.EqualFold(raw, "true")
		if raw == "0" || strings.EqualFold(raw, "false") {
			ok = false
		}
		f.Success = &ok
	}
	list, total, err := h.Store.ListLoginLogs(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

func (h *AuthHandler) ListQueryAudits(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可查看查询审计") {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	f := store.QueryAuditFilter{
		Username: c.Query("username"),
		Kind:     c.Query("kind"),
		Q:        c.Query("q"),
		Page:     page,
		PageSize: pageSize,
	}
	if raw := strings.TrimSpace(c.Query("success")); raw != "" {
		ok := raw == "1" || strings.EqualFold(raw, "true")
		if raw == "0" || strings.EqualFold(raw, "false") {
			ok = false
		}
		f.Success = &ok
	}
	list, total, err := h.Store.ListQueryAudits(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

// menusForUserInTenant 按租户成员角色出菜单；tenantID 空则空树（不再回退平台全量角色菜单）。
func (h *AuthHandler) Me(c *gin.Context) {
	start := time.Now()
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	tid := middleware.TenantID(c)
	imp := middleware.IsImpersonating(c)
	out := h.userPublicJSON(u, nil)
	_ = h.Store.EnsurePlatformAdminDefaultMembership(u.ID)
	menusStart := time.Now()
	h.applyUserAccess(out, u, tid, imp)
	menusMS := time.Since(menusStart).Milliseconds()
	tenants, _ := h.Store.ListUserTenants(u.ID)
	if imp {
		// 审计会话：顶栏列出全部活跃租户，避免只剩 UUID
		if all, err := h.Store.ListTenants(); err == nil {
			active := make([]store.Tenant, 0, len(all))
			for _, t := range all {
				if t.Status == store.TenantStatusActive {
					active = append(active, t)
				}
			}
			out["tenants"] = active
		}
	} else {
		out["tenants"] = tenants
	}
	if tid != "" {
		if t, _ := h.Store.GetTenant(tid); t != nil {
			out["current_tenant"] = t
		}
	}
	out["impersonating"] = imp
	log.Printf("me timing username=%s menus_ms=%d total_ms=%d", u.Username, menusMS, time.Since(start).Milliseconds())
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *AuthHandler) SelectTenant(c *gin.Context) {
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.TenantID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "tenant_id 必填"})
		return
	}
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	t, tenants, err := h.Store.PickLoginTenant(u.ID, req.TenantID)
	if err != nil || t == nil {
		msg := "无权进入该租户"
		if err != nil {
			msg = err.Error()
		}
		c.JSON(http.StatusForbidden, gin.H{"message": msg, "tenants": tenants})
		return
	}
	ttl := time.Duration(h.Cfg.App.JWTTTLHours) * time.Hour
	tok, err := middleware.SignToken(h.Cfg.App.JWTSecret, u.ID, u.Username, u.Role, t.ID, t.Code, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(t.ID, u.ID, "tenant.select", t.Code)
	out := h.userPublicJSON(u, nil)
	h.applyUserAccess(out, u, t.ID, false)
	out["current_tenant"] = t
	out["tenants"] = tenants
	c.JSON(http.StatusOK, gin.H{"token": tok, "user": out})
}

func (h *AuthHandler) userPublicJSON(u *store.User, menus any) gin.H {
	out := userPublicJSON(u, menus)
	if out == nil {
		out = gin.H{}
	}
	idle := 30
	if h != nil && h.Store != nil {
		idle = h.Store.LoginIdleTimeoutMin(30)
	}
	out["login_idle_timeout_min"] = idle
	if h != nil && h.Store != nil {
		out["user_watermark_enabled"] = h.Store.UserWatermarkEnabled()
	} else {
		out["user_watermark_enabled"] = false
	}
	// permissions 由 Me/SelectTenant/SSO 等按租户写入；此处不再默认灌平台 ListUserURNs。
	if _, ok := out["permissions"]; !ok {
		out["permissions"] = []string{}
	}
	if u != nil && len(u.RoleIDs) > 0 {
		out["role_ids"] = u.RoleIDs
	}
	return out
}

func userPublicJSON(u *store.User, menus any) gin.H {
	if u == nil {
		return gin.H{}
	}
	out := gin.H{
		"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
		"role": u.Role, "role_id": u.RoleID, "source": u.Source,
		"avatar": u.Avatar, "gender": u.Gender, "email": u.Email,
		"phone": u.Phone, "im_type": u.IMType, "im_account": u.IMAccount,
	}
	if menus != nil {
		out["menus"] = menus
	}
	return out
}

func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	var req struct {
		DisplayName string `json:"display_name"`
		Avatar      string `json:"avatar"`
		Gender      string `json:"gender"`
		Email       string `json:"email"`
		Phone       string `json:"phone"`
		IMType      string `json:"im_type"`
		IMAccount   string `json:"im_account"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "姓名必填"})
		return
	}
	if req.Email != "" && !strings.Contains(req.Email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "邮箱格式有误"})
		return
	}
	if len(req.Avatar) > 400*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "头像过大，请选择更小的图片"})
		return
	}
	if err := h.Store.UpdateUserProfile(u.ID, req.DisplayName, store.UserProfile{
		Avatar: req.Avatar, Gender: req.Gender, Email: req.Email,
		Phone: req.Phone, IMType: req.IMType, IMAccount: req.IMAccount,
	}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	latest, _ := h.Store.GetUserByID(u.ID)
	if latest == nil {
		latest = u
		latest.DisplayName = req.DisplayName
	}
	tid := middleware.TenantID(c)
	out := h.userPublicJSON(latest, nil)
	h.applyUserAccess(out, latest, tid, false)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": out})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	if uid, ok := c.Get(middleware.CtxUserID); ok {
		if s, _ := uid.(string); s != "" {
			middleware.ClearIdle(s)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	if u.Source == store.UserSourceLDAP {
		c.JSON(http.StatusBadRequest, gin.H{"message": "LDAP 账号请在目录服务中修改密码"})
		return
	}
	if u.Source == store.UserSourceSSO {
		c.JSON(http.StatusBadRequest, gin.H{"message": "SSO 账号请在统一认证中心修改密码"})
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" || len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "新密码至少 6 位"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.OldPassword)) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "原密码错误"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateUserPassword(u.ID, string(hash)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "密码已更新"})
}

const forgotPasswordOKMsg = "重置邮件已发送"

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	if at <= 0 {
		return "***"
	}
	local, domain := email[:at], email[at:]
	if len(local) <= 1 {
		return "*" + domain
	}
	if len(local) == 2 {
		return string(local[0]) + "*" + domain
	}
	return string(local[0]) + "***" + domain
}

var (
	forgotIPMu sync.Mutex
	forgotIP   = map[string][]time.Time{}
)

func allowForgotByIP(ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	forgotIPMu.Lock()
	defer forgotIPMu.Unlock()
	cut := now.Add(-15 * time.Minute)
	list := forgotIP[ip]
	kept := list[:0]
	for _, t := range list {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 10 {
		forgotIP[ip] = kept
		return false
	}
	forgotIP[ip] = append(kept, now)
	return true
}

// ForgotPassword 按用户名定位账号，向请求中填写的邮箱发送重置链接。
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	_ = c.ShouldBindJSON(&req)
	username := strings.TrimSpace(req.Username)
	email := strings.TrimSpace(req.Email)
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请填写要重置密码的用户名"})
		return
	}
	if email == "" || !strings.Contains(email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请填写有效的接收邮箱"})
		return
	}
	if !allowForgotByIP(c.ClientIP()) {
		c.JSON(http.StatusTooManyRequests, gin.H{"message": "请求过于频繁，请稍后再试"})
		return
	}

	smtpCfg, err := h.Store.GetSMTPConfig(h.encKey())
	if err != nil {
		log.Printf("forgot password smtp config: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"message": "邮件服务配置异常，请联系管理员重新保存 SMTP 密码"})
		return
	}
	if smtpCfg == nil || !smtpCfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "邮件服务未启用，请在「认证配置 → 邮件」打开「启用 SMTP」并保存"})
		return
	}
	if !smtpCfg.Ready() {
		c.JSON(http.StatusBadRequest, gin.H{"message": "邮件服务未配置完整（需填写服务器与发件人），请联系管理员"})
		return
	}

	u, _ := h.Store.GetUserByUsername(username)
	if u == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "未找到该用户名对应的账号"})
		return
	}
	if u.IsLoginBlocked() {
		c.JSON(http.StatusForbidden, gin.H{"message": u.LoginBlockedMessage()})
		return
	}
	if u.Source == store.UserSourceSSO {
		c.JSON(http.StatusBadRequest, gin.H{"message": "SSO 账号请前往统一认证中心重置密码"})
		return
	}

	n, _ := h.Store.CountRecentPasswordResets(u.ID, 15*time.Minute)
	if n >= 3 {
		c.JSON(http.StatusTooManyRequests, gin.H{"message": "该账号重置申请过于频繁，请 15 分钟后再试"})
		return
	}

	token, err := h.Store.NewPasswordResetToken(u.ID)
	if err != nil {
		log.Printf("forgot password token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "生成重置令牌失败，请稍后重试"})
		return
	}
	base := strings.TrimRight(strings.TrimSpace(smtpCfg.FrontendBaseURL), "/")
	if base == "" {
		base = "http://127.0.0.1:5174"
	}
	link := base + "/#/reset-password?token=" + url.QueryEscape(token)
	body := "您好 " + u.DisplayName + "（" + u.Username + "），\n\n" +
		"请点击以下链接重置 auto-dba 账号「" + u.Username + "」的密码（30 分钟内有效）：\n" + link + "\n\n" +
		"如非本人操作，请忽略本邮件。\n"
	if err := mail.Send(mail.Config{
		Host: smtpCfg.Host, Port: smtpCfg.Port, Username: smtpCfg.Username,
		Password: smtpCfg.Password, From: smtpCfg.From, UseTLS: smtpCfg.UseTLS,
	}, email, "重置 auto-dba 密码", body, "text/plain; charset=UTF-8"); err != nil {
		log.Printf("forgot password mail user=%s to=%s: %v", u.Username, email, err)
		c.JSON(http.StatusBadRequest, gin.H{"message": "发送重置邮件失败: " + err.Error()})
		return
	}
	log.Printf("forgot password mail sent user=%s to=%s", u.Username, email)
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "重置邮件已发送至 " + maskEmail(email) + "（账号：" + u.Username + "）",
	})
}

// ResetPassword 凭邮件 token 设置新密码（平台本地 / LDAP 目录）。
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" || len(req.NewPassword) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "新密码至少 6 位"})
		return
	}
	userID, err := h.Store.ConsumePasswordResetToken(req.Token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	u, err := h.Store.GetUserByID(userID)
	if err != nil || u == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "用户不存在"})
		return
	}
	if u.Source == store.UserSourceSSO {
		c.JSON(http.StatusBadRequest, gin.H{"message": "SSO 账号请在统一认证中心修改密码"})
		return
	}
	if u.IsLoginBlocked() {
		c.JSON(http.StatusForbidden, gin.H{"message": u.LoginBlockedMessage()})
		return
	}
	if u.Source == store.UserSourceLDAP {
		cfg, cfgErr := h.Store.GetLdapConfig(h.encKey())
		if cfgErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": ldapErrorMessage(cfgErr)})
			return
		}
		if cfg == nil || !cfg.Enabled {
			c.JSON(http.StatusBadRequest, gin.H{"message": "LDAP 未启用，无法重置目录密码"})
			return
		}
		if err := ldapauth.ResetPassword(cfg, u.Username, req.NewPassword); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		_ = h.Store.ClearLoginFailures(u.ID)
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "密码已重置，请使用新密码登录"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateUserPassword(u.ID, string(hash)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.ClearLoginFailures(u.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "密码已重置，请使用新密码登录"})
}
