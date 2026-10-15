package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"auto-dba/internal/secret"
)

const KeySMTPConfig = "smtp_config"

type SMTPConfig struct {
	Enabled         bool   `json:"enabled"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Password        string `json:"-"`
	PasswordEnc     string `json:"password_enc,omitempty"`
	From            string `json:"from"`
	UseTLS          bool   `json:"use_tls"`
	FrontendBaseURL string `json:"frontend_base_url"`
}

func DefaultSMTPConfig() *SMTPConfig {
	return &SMTPConfig{
		Port:            465,
		UseTLS:          true,
		FrontendBaseURL: "http://127.0.0.1:5174",
	}
}

func (d *DB) GetSMTPConfig(encKey string) (*SMTPConfig, error) {
	v, err := d.GetGlobalArg(KeySMTPConfig)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(v) == "" {
		return DefaultSMTPConfig(), nil
	}
	var c SMTPConfig
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return DefaultSMTPConfig(), nil
	}
	if c.Port <= 0 {
		c.Port = 465
	}
	if strings.TrimSpace(c.FrontendBaseURL) == "" {
		c.FrontendBaseURL = "http://127.0.0.1:5174"
	}
	if c.PasswordEnc == "" {
		return &c, nil
	}
	if encKey != "" {
		plain, decErr := secret.Decrypt(encKey, c.PasswordEnc)
		if decErr != nil {
			return &c, fmt.Errorf("SMTP 密码解密失败，请到认证配置重新保存")
		}
		c.Password = plain
	} else {
		c.Password = c.PasswordEnc
	}
	return &c, nil
}

func (d *DB) SetSMTPConfig(encKey string, c *SMTPConfig) error {
	if c == nil {
		return d.SetGlobalArg(KeySMTPConfig, "", "SMTP 邮件配置")
	}
	enc := c.PasswordEnc
	if c.Password != "" {
		if encKey != "" {
			var err error
			enc, err = secret.Encrypt(encKey, c.Password)
			if err != nil {
				return err
			}
		} else {
			enc = c.Password
		}
	}
	port := c.Port
	if port <= 0 {
		port = 465
	}
	base := strings.TrimRight(strings.TrimSpace(c.FrontendBaseURL), "/")
	if base == "" {
		base = "http://127.0.0.1:5174"
	}
	toStore := SMTPConfig{
		Enabled:         c.Enabled,
		Host:            strings.TrimSpace(c.Host),
		Port:            port,
		Username:        strings.TrimSpace(c.Username),
		PasswordEnc:     enc,
		From:            strings.TrimSpace(c.From),
		UseTLS:          c.UseTLS,
		FrontendBaseURL: base,
	}
	raw, err := json.Marshal(toStore)
	if err != nil {
		return err
	}
	return d.SetGlobalArg(KeySMTPConfig, string(raw), "SMTP 邮件配置（密码加密存储）")
}

func (c *SMTPConfig) Public() map[string]any {
	if c == nil {
		c = DefaultSMTPConfig()
	}
	return map[string]any{
		"enabled":           c.Enabled,
		"host":              c.Host,
		"port":              c.Port,
		"username":          c.Username,
		"from":              c.From,
		"use_tls":           c.UseTLS,
		"frontend_base_url": c.FrontendBaseURL,
		"password_set":      strings.TrimSpace(c.Password) != "" || strings.TrimSpace(c.PasswordEnc) != "",
	}
}

// Configured 表示 SMTP 参数已填齐（可测发），不要求 Enabled。
func (c *SMTPConfig) Configured() bool {
	if c == nil {
		return false
	}
	hostOK := strings.TrimSpace(c.Host) != ""
	addrOK := strings.Contains(strings.TrimSpace(c.From), "@") ||
		strings.Contains(strings.TrimSpace(c.Username), "@")
	passOK := strings.TrimSpace(c.Password) != "" || strings.TrimSpace(c.PasswordEnc) != ""
	return hostOK && addrOK && passOK
}

// Ready 表示已启用且可自动发信（工单/忘记密码等）；测试发信可用 Configured。
func (c *SMTPConfig) Ready() bool {
	if c == nil || !c.Enabled {
		return false
	}
	return strings.TrimSpace(c.Host) != "" &&
		(strings.Contains(strings.TrimSpace(c.From), "@") || strings.Contains(strings.TrimSpace(c.Username), "@"))
}
