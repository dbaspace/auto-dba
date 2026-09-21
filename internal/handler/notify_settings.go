package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type NotifyHandler struct {
	Store *store.DB
	Cfg   *config.Config
}

func (h *NotifyHandler) requireAdmin(c *gin.Context) bool {
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return false
	}
	code := strings.ToLower(u.Role)
	if u.RoleID != "" {
		if r, _ := h.Store.GetRole(u.RoleID); r != nil {
			code = strings.ToLower(r.Code)
		}
	}
	if code != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅管理员可配置认证"})
		return false
	}
	return true
}

func (h *NotifyHandler) Get(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	cfg, err := h.Store.GetNotifyConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cfg.Public()})
}

func (h *NotifyHandler) Update(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req store.NotifyConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	existing, _ := h.Store.GetNotifyConfig()
	if strings.TrimSpace(req.WecomSecret) == "" && existing != nil {
		req.WecomSecret = existing.WecomSecret
	}
	if err := h.Store.SetNotifyConfig(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
