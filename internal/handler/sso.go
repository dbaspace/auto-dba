package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/middleware"
	"auto-dba/internal/ssoauth"
	"auto-dba/internal/store"
)

type SsoHandler struct {
	Store *store.DB
	Cfg   *config.Config
}

func (h *SsoHandler) encKey() string {
	if h == nil || h.Cfg == nil {
		return ""
	}
	return h.Cfg.App.JWTSecret
}

func (h *SsoHandler) requireAdmin(c *gin.Context) bool {
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

func (h *SsoHandler) Get(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	cfg, err := h.Store.GetSsoConfig(h.encKey())
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

type ssoConfigReq struct {
	Enabled          bool   `json:"enabled"`
	GrantMode        string `json:"grant_mode"`
	BaseURL          string `json:"base_url"`
	CallbackURL      string `json:"callback_url"`
	AuthorizePath    string `json:"authorize_path"`
	TokenPath        string `json:"token_path"`
	UserInfoPath     string `json:"userinfo_path"`
	JWKSURI          string `json:"jwks_uri"`
	ClientID         string `json:"client_id"`
	ClientSecret     string `json:"client_secret"`
	Scope            string `json:"scope"`
	UsernameClaim    string `json:"username_claim"`
	DisplayNameClaim string `json:"display_name_claim"`
	EmailClaim       string `json:"email_claim"`
	TestUsername     string `json:"test_username"`
	TestPassword     string `json:"test_password"`
}

func (h *SsoHandler) Update(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req ssoConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	existing, _ := h.Store.GetSsoConfig(h.encKey())
	cfg := &store.SsoConfig{
		Enabled:          req.Enabled,
		GrantMode:        store.NormalizeSSOGrantMode(req.GrantMode),
		BaseURL:          strings.TrimSpace(req.BaseURL),
		CallbackURL:      strings.TrimSpace(req.CallbackURL),
		AuthorizePath:    strings.TrimSpace(req.AuthorizePath),
		TokenPath:        strings.TrimSpace(req.TokenPath),
		UserInfoPath:     strings.TrimSpace(req.UserInfoPath),
		JWKSURI:          strings.TrimSpace(req.JWKSURI),
		ClientID:         strings.TrimSpace(req.ClientID),
		Scope:            strings.TrimSpace(req.Scope),
		UsernameClaim:    strings.TrimSpace(req.UsernameClaim),
		DisplayNameClaim: strings.TrimSpace(req.DisplayNameClaim),
		EmailClaim:       strings.TrimSpace(req.EmailClaim),
	}
	if req.ClientSecret != "" {
		cfg.ClientSecret = req.ClientSecret
	} else if existing != nil {
		cfg.ClientSecret = existing.ClientSecret
		cfg.ClientSecretEnc = existing.ClientSecretEnc
	}
	if err := h.Store.SetSsoConfig(h.encKey(), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SsoHandler) Test(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req ssoConfigReq
	if c.Request != nil && c.Request.ContentLength > 0 {
		_ = c.ShouldBindJSON(&req)
	}
	var cfg *store.SsoConfig
	if strings.TrimSpace(req.BaseURL) != "" && strings.TrimSpace(req.ClientID) != "" {
		cfg = &store.SsoConfig{
			Enabled:          true,
			GrantMode:        store.NormalizeSSOGrantMode(req.GrantMode),
			BaseURL:          strings.TrimSpace(req.BaseURL),
			CallbackURL:      strings.TrimSpace(req.CallbackURL),
			AuthorizePath:    strings.TrimSpace(req.AuthorizePath),
			TokenPath:        strings.TrimSpace(req.TokenPath),
			UserInfoPath:     strings.TrimSpace(req.UserInfoPath),
			JWKSURI:          strings.TrimSpace(req.JWKSURI),
			ClientID:         strings.TrimSpace(req.ClientID),
			ClientSecret:     req.ClientSecret,
			Scope:            strings.TrimSpace(req.Scope),
			UsernameClaim:    strings.TrimSpace(req.UsernameClaim),
			DisplayNameClaim: strings.TrimSpace(req.DisplayNameClaim),
			EmailClaim:       strings.TrimSpace(req.EmailClaim),
		}
		if cfg.ClientSecret == "" {
			if existing, _ := h.Store.GetSsoConfig(h.encKey()); existing != nil {
				cfg.ClientSecret = existing.ClientSecret
			}
		}
	}
	if cfg == nil {
		var err error
		cfg, err = h.Store.GetSsoConfig(h.encKey())
		if err != nil || cfg == nil || strings.TrimSpace(cfg.BaseURL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请先填写并保存 SSO 配置（SSO 地址、Client ID），或在表单中填写完整信息后测试"})
			return
		}
		cfg.Enabled = true
	}
	testUser := strings.TrimSpace(req.TestUsername)
	if testUser != "" && req.TestPassword != "" {
		if cfg.UseAuthorizeGrant() {
			c.JSON(http.StatusBadRequest, gin.H{"message": "当前为授权码模式，账号测试请到登录页选择「SSO」后点击登录；此处可不填测试账号，仅探测 Token 可达"})
			return
		}
		info, err := ssoauth.Verify(cfg, testUser, req.TestPassword)
		if err != nil {
			msg := err.Error()
			if ssoauth.IsUnsupportedGrant(err) {
				msg = "IdP 不支持密码模式。请在认证配置将授权模式改为「授权码」，或联系 SSO 管理员开通 password。"
			}
			c.JSON(http.StatusBadRequest, gin.H{"message": msg})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"ok": true, "message": "SSO 用户验证成功（密码模式）",
			"username": info.Username, "display_name": info.DisplayName, "email": info.Email,
		})
		return
	}
	if err := ssoauth.Ping(cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	msg := "SSO Token 地址可达"
	if cfg.UseAuthorizeGrant() {
		msg = "SSO Token 地址可达（授权码模式：请到登录页选择 SSO 后点击登录验证）"
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": msg})
}
