package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/ldapauth"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type LdapHandler struct {
	Store *store.DB
	Cfg   *config.Config
}

func (h *LdapHandler) encKey() string {
	if h == nil || h.Cfg == nil {
		return ""
	}
	return h.Cfg.App.JWTSecret
}

func (h *LdapHandler) requireAdmin(c *gin.Context) bool {
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

func (h *LdapHandler) Get(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	cfg, err := h.Store.GetLdapConfig(h.encKey())
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := cfg.Public()
	if err != nil {
		out["warning"] = err.Error()
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

type ldapConfigReq struct {
	Enabled         bool   `json:"enabled"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	UseTLS          bool   `json:"use_tls"`
	BaseDN          string `json:"base_dn"`
	BindDN          string `json:"bind_dn"`
	BindPassword    string `json:"bind_password"`
	UserFilter      string `json:"user_filter"`
	DisplayNameAttr string `json:"display_name_attr"`
	TestUsername    string `json:"test_username"`
	TestPassword    string `json:"test_password"`
}

func (h *LdapHandler) Update(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req ldapConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	existing, _ := h.Store.GetLdapConfig(h.encKey())
	cfg := &store.LdapConfig{
		Enabled:         req.Enabled,
		Host:            strings.TrimSpace(req.Host),
		Port:            req.Port,
		UseTLS:          req.UseTLS,
		BaseDN:          strings.TrimSpace(req.BaseDN),
		BindDN:          strings.TrimSpace(req.BindDN),
		UserFilter:      strings.TrimSpace(req.UserFilter),
		DisplayNameAttr: strings.TrimSpace(req.DisplayNameAttr),
	}
	if req.BindPassword != "" {
		cfg.BindPassword = req.BindPassword
	} else if existing != nil {
		cfg.BindPassword = existing.BindPassword
		cfg.BindPasswordEnc = existing.BindPasswordEnc
	}
	if cfg.UserFilter == "" {
		cfg.UserFilter = store.DefaultLDAPUserFilter
	}
	if err := h.Store.SetLdapConfig(h.encKey(), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *LdapHandler) Test(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req ldapConfigReq
	if c.Request != nil && c.Request.ContentLength > 0 {
		_ = c.ShouldBindJSON(&req)
	}
	var cfg *store.LdapConfig
	if strings.TrimSpace(req.Host) != "" && req.Port > 0 && strings.TrimSpace(req.BindDN) != "" && req.BindPassword != "" {
		cfg = &store.LdapConfig{
			Enabled:         true,
			Host:            strings.TrimSpace(req.Host),
			Port:            req.Port,
			UseTLS:          req.UseTLS,
			BaseDN:          strings.TrimSpace(req.BaseDN),
			BindDN:          strings.TrimSpace(req.BindDN),
			BindPassword:    req.BindPassword,
			UserFilter:      strings.TrimSpace(req.UserFilter),
			DisplayNameAttr: strings.TrimSpace(req.DisplayNameAttr),
		}
		if cfg.UserFilter == "" {
			cfg.UserFilter = store.DefaultLDAPUserFilter
		}
	}
	if cfg == nil {
		var err error
		cfg, err = h.Store.GetLdapConfig(h.encKey())
		if err != nil || cfg == nil || strings.TrimSpace(cfg.Host) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请先填写并保存 LDAP 配置（服务器地址、端口、Bind DN、Bind 密码），或在表单中填写完整信息后测试"})
			return
		}
		if cfg.BindPassword == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "测试连接需要 Bind 密码，请填写后保存再测试"})
			return
		}
		cfg.Enabled = true
	}
	testUser := strings.TrimSpace(req.TestUsername)
	if testUser != "" && req.TestPassword != "" {
		displayName, err := ldapauth.Verify(cfg, testUser, req.TestPassword)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "LDAP 用户验证成功", "display_name": displayName})
		return
	}
	_, err := ldapauth.Verify(cfg, "_test_connection_", "x")
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "连接成功"})
		return
	}
	if strings.Contains(err.Error(), "user not found") || strings.Contains(err.Error(), "invalid password") {
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "连接与 Bind 成功"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
}
