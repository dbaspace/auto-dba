package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type ClientAccessHandler struct {
	Store *store.DB
}

type createGrantReq struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	TenantID  string `json:"tenant_id"`
	Scopes    string `json:"scopes"`
	ExpiresAt string `json:"expires_at"`
	Remark    string `json:"remark"`
	Days      int    `json:"days"` // >0 时按天计算过期
}

func (h *ClientAccessHandler) ListGrants(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可管理客户端访问授权") {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	list, total, err := h.Store.ListClientAccessGrants(c.Query("username"), c.Query("status"), page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

func (h *ClientAccessHandler) CreateGrant(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可管理客户端访问授权") {
		return
	}
	var req createGrantReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	uid := strings.TrimSpace(req.UserID)
	if uid == "" && strings.TrimSpace(req.Username) != "" {
		u, err := h.Store.GetUserByUsername(strings.TrimSpace(req.Username))
		if err != nil || u == nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "用户不存在"})
			return
		}
		uid = u.ID
	}
	if uid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "user_id 或 username 必填"})
		return
	}
	expires := strings.TrimSpace(req.ExpiresAt)
	if expires == "" && req.Days > 0 {
		expires = time.Now().UTC().Add(time.Duration(req.Days) * 24 * time.Hour).Format(time.RFC3339)
	}
	tid := strings.TrimSpace(req.TenantID)
	if tid == "" {
		if ts, _ := h.Store.ListUserTenants(uid); len(ts) > 0 {
			tid = ts[0].ID
		}
	}
	it := &store.ClientAccessGrant{
		UserID:    uid,
		TenantID:  tid,
		Scopes:    req.Scopes,
		ExpiresAt: expires,
		CreatedBy: middleware.Username(c),
		Remark:    req.Remark,
	}
	if err := h.Store.CreateClientAccessGrant(it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *ClientAccessHandler) RevokeGrant(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可管理客户端访问授权") {
		return
	}
	if err := h.Store.RevokeClientAccessGrant(c.Param("id"), middleware.Username(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *ClientAccessHandler) MyStatus(c *gin.Context) {
	g, err := h.Store.ActiveClientAccessGrantForUser(middleware.UserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":             h.Store.ClientAccessEnabled(),
		"enforce_machine_pat": h.Store.ClientAccessEnforceMachinePAT(),
		"has_grant":           g != nil,
		"grant":               g,
	})
}

type createPATReq struct {
	Name       string `json:"name"`
	ClientType string `json:"client_type"`
	Scopes     string `json:"scopes"` // 可选；默认跟随授权；credential 须全局开关
	ExpiresAt  string `json:"expires_at"`
	Days       int    `json:"days"`
}

func (h *ClientAccessHandler) ListMyPATs(c *gin.Context) {
	list, err := h.Store.ListClientPATsByUser(middleware.UserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *ClientAccessHandler) CreateMyPAT(c *gin.Context) {
	uid := middleware.UserID(c)
	g, err := h.Store.ActiveClientAccessGrantForUser(uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if g == nil {
		c.JSON(http.StatusForbidden, gin.H{"message": "未开通本地 AI/CLI/MCP 访问授权，请联系管理员"})
		return
	}
	var req createPATReq
	_ = c.ShouldBindJSON(&req)
	expires := strings.TrimSpace(req.ExpiresAt)
	if expires == "" && req.Days > 0 {
		expires = time.Now().UTC().Add(time.Duration(req.Days) * 24 * time.Hour).Format(time.RFC3339)
	}
	raw, prefix, err := store.GenerateClientPATRaw()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ct := strings.ToLower(strings.TrimSpace(req.ClientType))
	if ct == "" {
		ct = "mcp"
	}
	scopes := g.Scopes
	if s := strings.TrimSpace(req.Scopes); s != "" {
		scopes = s
	}
	it := &store.ClientPAT{
		UserID:      uid,
		GrantID:     g.ID,
		Name:        req.Name,
		TokenPrefix: prefix,
		Scopes:      scopes,
		ClientType:  ct,
		ExpiresAt:   expires,
	}
	if err := h.Store.CreateClientPAT(it, raw); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it, "token": raw, "hint": "请立即保存令牌，之后无法再查看明文"})
}

func (h *ClientAccessHandler) RevokeMyPAT(c *gin.Context) {
	if err := h.Store.RevokeClientPAT(c.Param("id"), middleware.UserID(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *ClientAccessHandler) ListAPIAudits(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可查看客户端接口审计") {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	list, total, err := h.Store.ListClientAPIAudits(store.ClientAPIAuditFilter{
		Username:   c.Query("username"),
		ClientType: c.Query("client_type"),
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}
