package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/secret"
)

type ldapConfigCache struct {
	mu     sync.Mutex
	cfg    *LdapConfig
	encKey string
	at     time.Time
}

var ldapCfgMem = &ldapConfigCache{}

const ldapConfigMemTTL = 60 * time.Second

const (
	KeyLdapConfig         = "ldap_config"
	UserSourcePlatform    = "platform"
	UserSourceLDAP        = "ldap"
	LDAPPlaceholderHash   = "$2a$10$LDAP.PLACEHOLDER.NO.LOGIN.VIA.PLATFORM"
	DefaultLDAPUserFilter = "(uid=%s)"
)

type LdapConfig struct {
	Enabled         bool   `json:"enabled"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	UseTLS          bool   `json:"use_tls"`
	BaseDN          string `json:"base_dn"`
	BindDN          string `json:"bind_dn"`
	BindPassword    string `json:"-"`
	BindPasswordEnc string `json:"bind_password_enc,omitempty"`
	UserFilter      string `json:"user_filter"`
	DisplayNameAttr string `json:"display_name_attr"`
}

func DefaultLdapConfig() *LdapConfig {
	return &LdapConfig{Port: 389, UserFilter: DefaultLDAPUserFilter}
}

func (d *DB) GetLdapConfig(encKey string) (*LdapConfig, error) {
	ldapCfgMem.mu.Lock()
	if ldapCfgMem.cfg != nil && ldapCfgMem.encKey == encKey && time.Since(ldapCfgMem.at) < ldapConfigMemTTL {
		cp := *ldapCfgMem.cfg
		ldapCfgMem.mu.Unlock()
		return &cp, nil
	}
	ldapCfgMem.mu.Unlock()

	v, err := d.GetGlobalArg(KeyLdapConfig)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(v) == "" {
		return DefaultLdapConfig(), nil
	}
	var c LdapConfig
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return DefaultLdapConfig(), nil
	}
	if c.Port <= 0 {
		c.Port = 389
	}
	if strings.TrimSpace(c.UserFilter) == "" {
		c.UserFilter = DefaultLDAPUserFilter
	}
	if c.BindPasswordEnc == "" {
		return &c, nil
	}
	if encKey != "" {
		plain, decErr := secret.Decrypt(encKey, c.BindPasswordEnc)
		if decErr != nil {
			return &c, fmt.Errorf("LDAP Bind 密码解密失败，请到 LDAP 配置页重新保存 Bind 密码")
		}
		c.BindPassword = plain
	} else {
		c.BindPassword = c.BindPasswordEnc
	}
	ldapCfgMem.mu.Lock()
	cp := c
	ldapCfgMem.cfg = &cp
	ldapCfgMem.encKey = encKey
	ldapCfgMem.at = time.Now()
	ldapCfgMem.mu.Unlock()
	return &c, nil
}

func (d *DB) SetLdapConfig(encKey string, c *LdapConfig) error {
	ldapCfgMem.mu.Lock()
	ldapCfgMem.cfg = nil
	ldapCfgMem.at = time.Time{}
	ldapCfgMem.mu.Unlock()
	if c == nil {
		return d.SetGlobalArg(KeyLdapConfig, "", "LDAP 登录配置")
	}
	enc := c.BindPasswordEnc
	if c.BindPassword != "" {
		if encKey != "" {
			var err error
			enc, err = secret.Encrypt(encKey, c.BindPassword)
			if err != nil {
				return err
			}
		} else {
			enc = c.BindPassword
		}
	}
	filter := strings.TrimSpace(c.UserFilter)
	if filter == "" {
		filter = DefaultLDAPUserFilter
	}
	toStore := LdapConfig{
		Enabled:         c.Enabled,
		Host:            strings.TrimSpace(c.Host),
		Port:            c.Port,
		UseTLS:          c.UseTLS,
		BaseDN:          strings.TrimSpace(c.BaseDN),
		BindDN:          strings.TrimSpace(c.BindDN),
		BindPasswordEnc: enc,
		UserFilter:      filter,
		DisplayNameAttr: strings.TrimSpace(c.DisplayNameAttr),
	}
	raw, err := json.Marshal(toStore)
	if err != nil {
		return err
	}
	return d.SetGlobalArg(KeyLdapConfig, string(raw), "LDAP 登录配置（Bind 密码加密存储）")
}

func (c *LdapConfig) Public() map[string]any {
	if c == nil {
		c = DefaultLdapConfig()
	}
	return map[string]any{
		"enabled":           c.Enabled,
		"host":              c.Host,
		"port":              c.Port,
		"use_tls":           c.UseTLS,
		"base_dn":           c.BaseDN,
		"bind_dn":           c.BindDN,
		"user_filter":       c.UserFilter,
		"display_name_attr": c.DisplayNameAttr,
		"bind_password_set": strings.TrimSpace(c.BindPassword) != "" || strings.TrimSpace(c.BindPasswordEnc) != "",
	}
}
