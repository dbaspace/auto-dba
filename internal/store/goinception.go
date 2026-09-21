package store

import (
	"encoding/json"
	"strings"

	"auto-dba/internal/secret"
)

const (
	KeyGoInception     = "ticket_goinception"
	descGoInceptionCfg = "MySQL/TiDB goInception 连接配置（密码加密）"
)

type GoInceptionSettings struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	Password    string `json:"-"`
	PasswordEnc string `json:"password_enc,omitempty"`
}

func DefaultGoInceptionSettings() *GoInceptionSettings {
	return &GoInceptionSettings{Port: 4000}
}

func (s *GoInceptionSettings) Ready() bool {
	return s != nil && s.Enabled && strings.TrimSpace(s.Host) != "" && s.Port > 0
}

func (d *DB) GetGoInceptionSettings(encKey string) (*GoInceptionSettings, error) {
	v, err := d.GetGlobalArg(KeyGoInception)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(v) == "" {
		return DefaultGoInceptionSettings(), nil
	}
	var c GoInceptionSettings
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return DefaultGoInceptionSettings(), nil
	}
	if c.Port <= 0 {
		c.Port = 4000
	}
	if c.PasswordEnc != "" {
		if encKey != "" {
			if plain, decErr := secret.Decrypt(encKey, c.PasswordEnc); decErr == nil {
				c.Password = plain
			}
		} else {
			c.Password = c.PasswordEnc
		}
	}
	return &c, nil
}

func (d *DB) SetGoInceptionSettings(encKey string, c *GoInceptionSettings) error {
	if c == nil {
		return d.SetGlobalArg(KeyGoInception, "", descGoInceptionCfg)
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
	toStore := GoInceptionSettings{
		Enabled:     c.Enabled,
		Host:        strings.TrimSpace(c.Host),
		Port:        c.Port,
		User:        strings.TrimSpace(c.User),
		PasswordEnc: enc,
	}
	if toStore.Port <= 0 {
		toStore.Port = 4000
	}
	raw, err := json.Marshal(toStore)
	if err != nil {
		return err
	}
	return d.SetGlobalArg(KeyGoInception, string(raw), descGoInceptionCfg)
}

func (s *GoInceptionSettings) Public() map[string]any {
	if s == nil {
		s = DefaultGoInceptionSettings()
	}
	return map[string]any{
		"enabled":      s.Enabled,
		"host":         s.Host,
		"port":         s.Port,
		"user":         s.User,
		"password_set": strings.TrimSpace(s.Password) != "" || strings.TrimSpace(s.PasswordEnc) != "",
	}
}
