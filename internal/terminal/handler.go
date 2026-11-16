package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
	"auto-dba/pkg/webtty"
	"auto-dba/pkg/webtty/conn"
	"auto-dba/pkg/webtty/terminal/localcommand"
)

const ActionNodeTerminal = "node.terminal"

type Handler struct {
	Store  *store.DB
	Cfg    *config.Config
	Secret string
}

type openReq struct {
	ActionType string            `json:"action_type"`
	ActionName string            `json:"action_name"`
	Args       map[string]string `json:"args"`
	Envs       map[string]string `json:"envs"`
}

type execContent struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Envs        map[string]string `json:"envs"`
	PermitWrite bool              `json:"permit_write"`
	Title       string            `json:"title"`
	MaxLifetime int               `json:"max_lifetime_sec"`
	MaxAbsolute int               `json:"max_absolute_sec"`
}

type sessionClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	Subprotocols:    []string{"webtty"},
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func (h *Handler) enabled() bool {
	return h != nil && h.Cfg != nil && h.Cfg.Terminal.Enabled
}

// Open POST /api/v1/terminal — 对齐 neptune-terminal 创建会话
func (h *Handler) Open(c *gin.Context) {
	if !h.enabled() {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "终端功能未启用"})
		return
	}
	var req openReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "参数无效"})
		return
	}
	if strings.TrimSpace(req.ActionType) == "" {
		req.ActionType = "command"
	}
	if req.ActionType != "command" || req.ActionName != ActionNodeTerminal {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "仅支持 action_name=node.terminal"})
		return
	}
	host := strings.TrimSpace(req.Args["host"])
	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "args.host 必填"})
		return
	}
	inst := h.findAuthorizedHost(middleware.TenantID(c), middleware.Username(c), middleware.Role(c), host)
	if inst == nil {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "host 未登记为已授权主机实例"})
		return
	}
	sshUser := strings.TrimSpace(inst.Username)
	if sshUser == "" {
		sshUser = h.Cfg.Terminal.SSHUser
	}
	sshPort := inst.Port
	if sshPort <= 0 {
		sshPort = 22
	}
	token, _, err := h.createSession(middleware.Username(c), middleware.TenantID(c), inst.ID, host, sshUser, sshPort, inst.IDC, req.Envs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"token": token}})
}

// OpenInstance POST /api/v1/instances/:id/open-terminal
func (h *Handler) OpenInstance(c *gin.Context) {
	if !h.enabled() {
		c.JSON(http.StatusBadRequest, gin.H{"message": "终端功能未启用"})
		return
	}
	inst, err := h.Store.GetInstanceInTenant(c.Param("id"), middleware.TenantID(c))
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	if !store.IsHostInstanceType(inst.DBType) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持主机类型实例"})
		return
	}
	viewer := h.Store.ViewerOfInTenant(middleware.Username(c), middleware.Role(c), inst.TenantID)
	if !inst.VisibleTo(viewer) {
		c.JSON(http.StatusForbidden, gin.H{"message": "未授权访问该主机终端"})
		return
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例未配置 Host"})
		return
	}
	// 去掉可能的 host:port 仅保留主机
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host, "]") {
		portPart := host[i+1:]
		allDigit := len(portPart) > 0
		for _, ch := range portPart {
			if ch < '0' || ch > '9' {
				allDigit = false
				break
			}
		}
		if allDigit {
			host = host[:i]
		}
	}
	sshUser := strings.TrimSpace(inst.Username)
	if sshUser == "" {
		sshUser = h.Cfg.Terminal.SSHUser
	}
	sshPort := inst.Port
	if sshPort <= 0 {
		sshPort = 22
	}
	token, sessID, err := h.createSession(middleware.Username(c), middleware.TenantID(c), inst.ID, host, sshUser, sshPort, inst.IDC, map[string]string{
		"SERVICENAME": strings.TrimSpace(inst.Name),
		"USERNAME":    middleware.Username(c),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	termURL := "/api/v1/terminal?token=" + token
	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"token":        token,
			"terminal_url": termURL,
			"session_id":   sessID,
			"host":         host,
			"ssh_user":     sshUser,
			"ssh_port":     sshPort,
			"action_name":  ActionNodeTerminal,
			"instance_id":  inst.ID,
		},
	})
}

func (h *Handler) createSession(user, tenantID, instanceID, host, sshUser string, sshPort int, envCode string, envs map[string]string) (token, sessID string, err error) {
	tc := h.Cfg.Terminal
	script := filepath.Join(tc.ScriptsDir, "node-terminal.sh")
	if _, err := os.Stat(script); err != nil {
		return "", "", fmt.Errorf("终端脚本不存在: %s", script)
	}
	key := tc.ResolveSSHIdentity(envCode)
	if key == "" {
		return "", "", fmt.Errorf("未配置 terminal.ssh_identity_file / ssh_key_profiles")
	}
	if _, err := os.Stat(key); err != nil {
		return "", "", fmt.Errorf("SSH 私钥不存在: %s（请在 Nacos terminal.ssh_private_key / ssh_key_profiles 配置）", key)
	}
	if strings.TrimSpace(sshUser) == "" {
		sshUser = tc.SSHUser
	}
	if sshPort <= 0 {
		sshPort = 22
	}
	idle, abs := sessionLimits(tc)
	ec := execContent{
		Command:     "/bin/sh",
		Args:        []string{script, host, sshUser, key, fmt.Sprintf("%d", sshPort)},
		Envs:        envs,
		PermitWrite: true,
		Title:       "Node Terminal — " + host + " — " + filepath.Base(key),
		MaxLifetime: int(idle / time.Second),
		MaxAbsolute: int(abs / time.Second),
	}
	raw, _ := json.Marshal(ec)
	// 重连窗口先按空闲时间；有输入或输出时再往后推，但不超过绝对上限。
	expires := time.Now().UTC().Add(idle).Format(time.RFC3339)
	s := &store.TerminalSession{
		TenantID:    tenantID,
		ActionName:  ActionNodeTerminal,
		ExecContent: string(raw),
		Status:      store.TermStatusCreated,
		CreatedBy:   user,
		InstanceID:  instanceID,
		Host:        host,
		ExpiresAt:   expires,
	}
	if err := h.Store.CreateTerminalSession(s); err != nil {
		return "", "", err
	}
	tok, err := h.signSessionToken(s.ID, abs)
	if err != nil {
		return "", "", err
	}
	return tok, s.ID, nil
}

func (h *Handler) findAuthorizedHost(tenantID, username, role, host string) *store.Instance {
	host = strings.TrimSpace(host)
	if host == "" || h.Store == nil {
		return nil
	}
	list, err := h.Store.ListInstancesInTenant(tenantID, "host", "")
	if err != nil {
		return nil
	}
	match := func(it store.Instance) bool {
		viewer := h.Store.ViewerOfInTenant(username, role, it.TenantID)
		if !store.IsHostInstanceType(it.DBType) || !it.VisibleTo(viewer) {
			return false
		}
		if strings.EqualFold(strings.TrimSpace(it.Host), host) {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(it.DomainName), host) {
			return true
		}
		if dn := strings.TrimSpace(it.DomainName); dn != "" {
			if i := strings.LastIndex(dn, ":"); i > 0 && strings.EqualFold(dn[:i], host) {
				return true
			}
		}
		return false
	}
	for i := range list {
		if match(list[i]) {
			return &list[i]
		}
	}
	return nil
}

func (h *Handler) hostRegistered(tenantID, host string) bool {
	return h.findAuthorizedHost(tenantID, "", "admin", host) != nil
}

func (h *Handler) signSessionToken(sessionID string, ttl time.Duration) (string, error) {
	now := time.Now()
	c := sessionClaims{
		SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "terminal",
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(h.Secret))
}

func (h *Handler) parseSessionToken(raw string) (string, error) {
	claims := &sessionClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		return []byte(h.Secret), nil
	})
	if err != nil || !tok.Valid {
		return "", fmt.Errorf("invalid token")
	}
	if strings.TrimSpace(claims.SessionID) == "" {
		return "", fmt.Errorf("session id missing")
	}
	return claims.SessionID, nil
}

// Page GET /api/v1/terminal?token=
func (h *Handler) Page(c *gin.Context) {
	if !h.enabled() {
		c.String(http.StatusBadRequest, "terminal disabled")
		return
	}
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		c.String(http.StatusBadRequest, "token required")
		return
	}
	if _, err := h.parseSessionToken(token); err != nil {
		c.String(http.StatusUnauthorized, "invalid token")
		return
	}
	index := filepath.Join(h.Cfg.Terminal.StaticDir, "index.html")
	c.File(index)
}

// WS GET /api/v1/terminal/ws?token=
func (h *Handler) WS(c *gin.Context) {
	if !h.enabled() {
		c.JSON(http.StatusBadRequest, gin.H{"message": "terminal disabled"})
		return
	}
	token := strings.TrimSpace(c.Query("token"))
	sessID, err := h.parseSessionToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid token"})
		return
	}
	sess, err := h.Store.GetTerminalSession(sessID)
	if err != nil || sess == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "session not found"})
		return
	}
	if sess.Status == store.TermStatusExpired || sess.Status == store.TermStatusFailed {
		h.writeTerminalNotice(c, sess, sess.Status)
		return
	}
	if exp, err := time.Parse(time.RFC3339, sess.ExpiresAt); err == nil && time.Now().UTC().After(exp) {
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusExpired)
		h.writeTerminalNotice(c, sess, store.TermStatusExpired)
		return
	}

	var ec execContent
	if err := json.Unmarshal([]byte(sess.ExecContent), &ec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "bad exec content"})
		return
	}

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	stopPing := keepAliveWS(ws, 20*time.Second, conn.ReadIdle)
	defer stopPing()

	_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusRunning)

	idleSec, absSec := ec.MaxLifetime, ec.MaxAbsolute
	if h.Cfg != nil {
		if idleSec <= 0 {
			idleSec = h.Cfg.Terminal.MaxLifetimeSec
		}
		if absSec <= 0 {
			absSec = h.Cfg.Terminal.MaxAbsoluteSec
		}
	}
	if idleSec <= 0 {
		idleSec = 1800
	}
	if absSec <= 0 {
		absSec = 8 * 3600
	}
	if absSec < idleSec {
		absSec = idleSec
	}
	created := time.Now()
	if t, err := time.Parse(time.RFC3339, sess.CreatedAt); err == nil {
		created = t
	}
	absoluteAt := created.Add(time.Duration(absSec) * time.Second)
	if !time.Now().Before(absoluteAt) {
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusExpired)
		_, _ = conn.WrapperWs(ws).Write(conn.Output, []byte(terminalStopNotice(sess, "absolute", idleSec)))
		return
	}
	dl := newSessionDeadline(time.Now(), time.Duration(idleSec)*time.Second, absoluteAt)
	if exp, persist := dl.Touch(time.Now()); persist && h.Store != nil {
		_ = h.Store.TouchTerminalSessionExpiry(sess.ID, exp.UTC().Format(time.RFC3339))
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	webConn := conn.WrapperWs(ws)
	var endKind atomic.Value
	var endOnce sync.Once
	go watchSessionDeadline(ctx, cancel, dl, func(kind string) {
		endOnce.Do(func() {
			endKind.Store(kind)
			_, _ = webConn.Write(conn.Output, []byte(terminalStopNotice(sess, kind, idleSec)))
			_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusExpired)
		})
	})

	opts := []localcommand.Option{localcommand.WithEnvs(ec.Envs)}
	lcmd, err := localcommand.New(ctx, ec.Command, ec.Args, opts...)
	if err != nil {
		_, _ = webConn.Write(conn.Output, []byte("failed to start: "+err.Error()))
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusFailed)
		return
	}

	wtOpts := []webtty.Option{
		webtty.WithWindowTitle(ec.Title),
		webtty.WithReconnect(5),
	}
	if ec.PermitWrite {
		wtOpts = append(wtOpts, webtty.WithPermitWrite())
	}
	wtOpts = append(wtOpts, webtty.WithOnActivity(func() {
		exp, persist := dl.Touch(time.Now())
		if persist && h.Store != nil {
			_ = h.Store.TouchTerminalSessionExpiry(sess.ID, exp.UTC().Format(time.RFC3339))
		}
	}))
	wt, err := webtty.New(webConn, lcmd, wtOpts...)
	if err != nil {
		lcmd.Close()
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusFailed)
		return
	}
	runErr := wt.Run(ctx)
	if kind, _ := endKind.Load().(string); kind != "" {
		return
	}
	if runErr != nil && ctx.Err() == nil {
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusFailed)
		return
	}
	_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusFinished)
}

// writeTerminalNotice 把已结束会话的原因写进已升级的 WebSocket。
// 升级前直接返回 4xx 时，页面会当成「一次都没 OPEN」并盖上红条，ssh 的真实报错看不到。
func (h *Handler) writeTerminalNotice(c *gin.Context, sess *store.TerminalSession, status string) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	_, _ = conn.WrapperWs(ws).Write(conn.Output, []byte(terminalNotice(sess, status)))
}

func terminalEndpoint(sess *store.TerminalSession) (host, user, key string) {
	if sess == nil {
		return "", "", ""
	}
	host = sess.Host
	var ec execContent
	if json.Unmarshal([]byte(sess.ExecContent), &ec) != nil {
		return host, "", ""
	}
	// 实际会话参数是 script, host, user, key, port。旧通知测试和更早的记录是 host, user, key。
	if len(ec.Args) >= 4 {
		if strings.TrimSpace(host) == "" {
			host = ec.Args[1]
		}
		return host, ec.Args[2], ec.Args[3]
	}
	if strings.TrimSpace(host) == "" && len(ec.Args) > 0 {
		host = ec.Args[0]
	}
	if len(ec.Args) > 1 {
		user = ec.Args[1]
	}
	if len(ec.Args) > 2 {
		key = ec.Args[2]
	}
	return host, user, key
}

func terminalNotice(sess *store.TerminalSession, status string) string {
	host, user, key := terminalEndpoint(sess)
	switch status {
	case store.TermStatusExpired:
		return fmt.Sprintf("终端已空闲过久或超过最长存活时间，不是平台登录过期。主机 %s，用户 %s，私钥 %s。请从主机列表重新打开。\r\n", host, user, key)
	default:
		return fmt.Sprintf("远端 SSH 已退出，会话标为失败。这不是平台登录过期。主机 %s，用户 %s，私钥 %s。请用这把私钥核对目标机 authorized_keys，再从主机列表重新打开。\r\n", host, user, key)
	}
}

func terminalStopNotice(sess *store.TerminalSession, kind string, idleSec int) string {
	host, user, key := terminalEndpoint(sess)
	if kind == "idle" {
		mins := idleSec / 60
		if mins < 1 {
			mins = 1
		}
		return fmt.Sprintf("终端已空闲超过 %d 分钟，连接已关闭。这不是平台登录过期。主机 %s，用户 %s，私钥 %s。请从主机列表重新打开。\r\n", mins, host, user, key)
	}
	return fmt.Sprintf("终端已超过最长存活时间，不是平台登录过期。主机 %s，用户 %s，私钥 %s。请从主机列表重新打开。\r\n", host, user, key)
}

func sessionLimits(tc config.TerminalConfig) (idle, abs time.Duration) {
	idleSec := tc.MaxLifetimeSec
	if idleSec <= 0 {
		idleSec = 1800
	}
	absSec := tc.MaxAbsoluteSec
	if absSec <= 0 {
		absSec = 8 * 3600
	}
	if absSec < idleSec {
		absSec = idleSec
	}
	return time.Duration(idleSec) * time.Second, time.Duration(absSec) * time.Second
}

// watchSessionDeadline 空闲到点或绝对上限到点时通知并取消会话。有活动时只重置空闲计时。
func watchSessionDeadline(ctx context.Context, cancel context.CancelFunc, d *sessionDeadline, onStop func(kind string)) {
	if d == nil || cancel == nil {
		return
	}
	absWait := time.Until(d.absolute)
	if absWait < 0 {
		absWait = 0
	}
	absTimer := time.NewTimer(absWait)
	defer absTimer.Stop()
	idleWait := time.Until(d.IdleAt())
	if idleWait < 0 {
		idleWait = 0
	}
	idleTimer := time.NewTimer(idleWait)
	defer idleTimer.Stop()
	resetIdle := func() {
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		wait := time.Until(d.IdleAt())
		if wait < 0 {
			wait = 0
		}
		idleTimer.Reset(wait)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-absTimer.C:
			if onStop != nil {
				onStop("absolute")
			}
			cancel()
			return
		case <-idleTimer.C:
			if d.IdleDue(time.Now()) {
				if onStop != nil {
					onStop("idle")
				}
				cancel()
				return
			}
			resetIdle()
		case <-d.ping:
			resetIdle()
		}
	}
}

// keepAliveWS 发协议层 Ping，不依赖前台标签页里的 JS 定时器。返回停止函数。
func keepAliveWS(ws *websocket.Conn, every, idle time.Duration) func() {
	if ws == nil {
		return func() {}
	}
	if every <= 0 {
		every = 20 * time.Second
	}
	if idle <= 0 {
		idle = 60 * time.Second
	}
	_ = ws.SetReadDeadline(time.Now().Add(idle))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(idle))
	})
	done := make(chan struct{})
	var once sync.Once
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				deadline := time.Now().Add(10 * time.Second)
				if err := ws.WriteControl(websocket.PingMessage, []byte("ping"), deadline); err != nil {
					return
				}
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// MountStatic 托管 gotty 静态资源（挂在 /api/v1/terminal/static，便于同源代理）
func (h *Handler) MountStatic(e *gin.Engine) {
	if h == nil || h.Cfg == nil {
		return
	}
	dir := h.Cfg.Terminal.StaticDir
	if dir == "" {
		return
	}
	e.Static("/api/v1/terminal/static", dir)
}
