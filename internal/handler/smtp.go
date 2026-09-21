package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/mail"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type SMTPHandler struct {
	Store *store.DB
	Cfg   *config.Config
}

func (h *SMTPHandler) encKey() string {
	if h == nil || h.Cfg == nil {
		return ""
	}
	return h.Cfg.App.JWTSecret
}

func (h *SMTPHandler) requireAdmin(c *gin.Context) bool {
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

func (h *SMTPHandler) Get(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	cfg, err := h.Store.GetSMTPConfig(h.encKey())
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

type smtpConfigReq struct {
	Enabled         bool   `json:"enabled"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	From            string `json:"from"`
	UseTLS          bool   `json:"use_tls"`
	FrontendBaseURL string `json:"frontend_base_url"`
	TestTo          string `json:"test_to"`
}

func (h *SMTPHandler) Update(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req smtpConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	existing, _ := h.Store.GetSMTPConfig(h.encKey())
	cfg := &store.SMTPConfig{
		Enabled:         req.Enabled,
		Host:            strings.TrimSpace(req.Host),
		Port:            req.Port,
		Username:        strings.TrimSpace(req.Username),
		From:            strings.TrimSpace(req.From),
		UseTLS:          req.UseTLS,
		FrontendBaseURL: strings.TrimSpace(req.FrontendBaseURL),
	}
	if req.Password != "" {
		cfg.Password = req.Password
	} else if existing != nil {
		cfg.Password = existing.Password
		cfg.PasswordEnc = existing.PasswordEnc
	}
	if err := h.Store.SetSMTPConfig(h.encKey(), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SMTPHandler) Test(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req smtpConfigReq
	if c.Request != nil && c.Request.ContentLength > 0 {
		_ = c.ShouldBindJSON(&req)
	}
	cfg, err := h.buildSMTPForSend(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	to := strings.TrimSpace(req.TestTo)
	if to == "" {
		to = strings.TrimSpace(cfg.From)
	}
	if to == "" {
		to = strings.TrimSpace(cfg.Username)
	}
	if err := mail.Send(mail.Config{
		Host: cfg.Host, Port: cfg.Port, Username: cfg.Username,
		Password: cfg.Password, From: cfg.From, UseTLS: cfg.UseTLS,
	}, to, "auto-dba SMTP 测试", "这是一封来自 auto-dba 的 SMTP 测试邮件。", "text/plain; charset=UTF-8"); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "测试邮件已发送至 " + to})
}

func (h *SMTPHandler) buildSMTPForSend(req smtpConfigReq) (*store.SMTPConfig, error) {
	if strings.TrimSpace(req.Host) != "" && req.Password != "" {
		cfg := &store.SMTPConfig{
			Enabled:  true,
			Host:     strings.TrimSpace(req.Host),
			Port:     req.Port,
			Username: strings.TrimSpace(req.Username),
			Password: req.Password,
			From:     strings.TrimSpace(req.From),
			UseTLS:   req.UseTLS,
		}
		if cfg.Port <= 0 {
			cfg.Port = 465
		}
		return cfg, nil
	}
	cfg, err := h.Store.GetSMTPConfig(h.encKey())
	if err != nil {
		return nil, err
	}
	if cfg == nil || strings.TrimSpace(cfg.Host) == "" {
		return nil, errors.New("请先填写并保存 SMTP 配置")
	}
	if cfg.Password == "" {
		return nil, errors.New("测试发信需要 SMTP 密码，请填写后保存再测试")
	}
	return cfg, nil
}
