package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/sessionstore"
	"auto-dba/internal/store"
)

// LoginSessionHandler 管理端「登录会话」与用户「我的登录」（数据只读 Redis）。
type LoginSessionHandler struct {
	Store    *store.DB
	Sessions *sessionstore.Store
}

func (h *LoginSessionHandler) requireSessions(c *gin.Context) bool {
	if h == nil || h.Sessions == nil || !h.Sessions.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"message": "登录会话监控未启用（请在 Nacos 配置 redis.enabled=true）",
			"code":    "session_store_disabled",
		})
		return false
	}
	return true
}

// Stats GET /auth/sessions/stats
func (h *LoginSessionHandler) Stats(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可查看登录会话") {
		return
	}
	if !h.requireSessions(c) {
		return
	}
	n, err := h.Sessions.CountOnline(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"valid_user_count": n,
			"label":            "有效会话用户数",
			"redis":            h.Sessions.AddrHint(),
		},
	})
}

// List GET /auth/sessions?page=&page_size=&username=
func (h *LoginSessionHandler) List(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可查看登录会话") {
		return
	}
	if !h.requireSessions(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	unameFilter := strings.ToLower(strings.TrimSpace(c.Query("username")))
	offset := int64((page - 1) * size)
	ids, total, err := h.Sessions.ListOnlineUsers(c.Request.Context(), offset, int64(size))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	rows := make([]gin.H, 0)
	for _, uid := range ids {
		sessList, e := h.Sessions.SessionsOfUser(c.Request.Context(), uid)
		if e != nil || len(sessList) == 0 {
			continue
		}
		for _, s := range sessList {
			if unameFilter != "" && !strings.Contains(strings.ToLower(s.Username), unameFilter) {
				continue
			}
			rows = append(rows, gin.H{
				"user_id":    s.UserID,
				"username":   s.Username,
				"tenant_id":  s.TenantID,
				"jti":        s.JTI,
				"ip":         s.IP,
				"user_agent": s.UA,
				"online_at":  s.OnlineAt,
				"expire_at":  s.ExpireAt,
				"kind":       s.Kind,
				"status":     "会话有效",
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "total": total, "page": page, "page_size": size})
}

// Mine GET /auth/sessions/mine
func (h *LoginSessionHandler) Mine(c *gin.Context) {
	if !h.requireSessions(c) {
		return
	}
	uid := middleware.UserID(c)
	curJTI := middleware.JTI(c)
	list, err := h.Sessions.SessionsOfUser(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	rows := make([]gin.H, 0, len(list))
	for _, s := range list {
		rows = append(rows, gin.H{
			"jti":        s.JTI,
			"tenant_id":  s.TenantID,
			"ip":         s.IP,
			"user_agent": s.UA,
			"online_at":  s.OnlineAt,
			"expire_at":  s.ExpireAt,
			"kind":       s.Kind,
			"status":     "会话有效",
			"current":    s.JTI == curJTI,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

type kickReq struct {
	UserID string `json:"user_id"`
	JTI    string `json:"jti"`
}

// Kick POST /auth/sessions/kick  {user_id} 或 {user_id,jti}
func (h *LoginSessionHandler) Kick(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可强制下线") {
		return
	}
	if !h.requireSessions(c) {
		return
	}
	var req kickReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	uid := strings.TrimSpace(req.UserID)
	jti := strings.TrimSpace(req.JTI)
	if uid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "user_id 必填"})
		return
	}
	actor := middleware.UserID(c)
	ctx := c.Request.Context()
	if jti != "" {
		if err := h.Sessions.RevokeJTI(ctx, uid, jti); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
			return
		}
		if h.Store != nil {
			if err := h.Store.MarkUserSessionOffline(jti, "kicked", actor); err != nil {
				log.Printf("login session kick mirror offline jti=%s: %v", jti, err)
			}
		}
	} else {
		if err := h.Sessions.RevokeUser(ctx, uid); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
			return
		}
		if h.Store != nil {
			if err := h.Store.MarkUserSessionsOfflineAll(uid, "kicked_all", actor); err != nil {
				log.Printf("login session kick-all mirror offline uid=%s: %v", uid, err)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RevokeOther POST /auth/sessions/revoke-others 下线本账号其他端
func (h *LoginSessionHandler) RevokeOther(c *gin.Context) {
	if !h.requireSessions(c) {
		return
	}
	uid := middleware.UserID(c)
	cur := middleware.JTI(c)
	list, err := h.Sessions.SessionsOfUser(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"message": err.Error()})
		return
	}
	n := 0
	for _, s := range list {
		if s.JTI == cur {
			continue
		}
		if e := h.Sessions.RevokeJTI(c.Request.Context(), uid, s.JTI); e == nil {
			n++
			if h.Store != nil {
				_ = h.Store.MarkUserSessionOffline(s.JTI, "revoke_others", uid)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "revoked": n})
}
