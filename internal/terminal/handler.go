package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	token, _, err := h.createSession(middleware.Username(c), middleware.TenantID(c), inst.ID, host, sshUser, sshPort, req.Envs)
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
	token, sessID, err := h.createSession(middleware.Username(c), middleware.TenantID(c), inst.ID, host, sshUser, sshPort, map[string]string{
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

func (h *Handler) createSession(user, tenantID, instanceID, host, sshUser string, sshPort int, envs map[string]string) (token, sessID string, err error) {
	tc := h.Cfg.Terminal
	script := filepath.Join(tc.ScriptsDir, "node-terminal.sh")
	if _, err := os.Stat(script); err != nil {
		return "", "", fmt.Errorf("终端脚本不存在: %s", script)
	}
	key := tc.SSHIdentityFile
	if _, err := os.Stat(key); err != nil {
		return "", "", fmt.Errorf("SSH 私钥不存在: %s（请在 config.terminal.ssh_identity_file 配置）", key)
	}
	if strings.TrimSpace(sshUser) == "" {
		sshUser = tc.SSHUser
	}
	if sshPort <= 0 {
		sshPort = 22
	}
	ec := execContent{
		Command:     "/bin/sh",
		Args:        []string{script, host, sshUser, key, fmt.Sprintf("%d", sshPort)},
		Envs:        envs,
		PermitWrite: true,
		Title:       "Node Terminal — " + host,
		MaxLifetime: tc.MaxLifetimeSec,
	}
	raw, _ := json.Marshal(ec)
	expires := time.Now().UTC().Add(time.Duration(tc.MaxLifetimeSec) * time.Second).Format(time.RFC3339)
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
	tok, err := h.signSessionToken(s.ID, time.Duration(tc.MaxLifetimeSec)*time.Second)
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
		c.JSON(http.StatusBadRequest, gin.H{"message": "session " + sess.Status})
		return
	}
	if exp, err := time.Parse(time.RFC3339, sess.ExpiresAt); err == nil && time.Now().UTC().After(exp) {
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusExpired)
		c.JSON(http.StatusBadRequest, gin.H{"message": "session expired"})
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

	_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusRunning)

	maxLife := ec.MaxLifetime
	if maxLife <= 0 {
		maxLife = h.Cfg.Terminal.MaxLifetimeSec
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(maxLife)*time.Second)
	defer cancel()

	opts := []localcommand.Option{localcommand.WithEnvs(ec.Envs)}
	lcmd, err := localcommand.New(ctx, ec.Command, ec.Args, opts...)
	if err != nil {
		_, _ = conn.WrapperWs(ws).Write(conn.Output, []byte("failed to start: "+err.Error()))
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusFailed)
		return
	}

	wtOpts := []webtty.Option{webtty.WithWindowTitle(ec.Title)}
	if ec.PermitWrite {
		wtOpts = append(wtOpts, webtty.WithPermitWrite())
	}
	wt, err := webtty.New(conn.WrapperWs(ws), lcmd, wtOpts...)
	if err != nil {
		lcmd.Close()
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusFailed)
		return
	}
	runErr := wt.Run(ctx)
	if runErr != nil && ctx.Err() == nil {
		_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusFailed)
		return
	}
	_ = h.Store.UpdateTerminalSessionStatus(sess.ID, store.TermStatusFinished)
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
