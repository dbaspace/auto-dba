package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"auto-dba/internal/secret"
)

const (
	KeySsoConfig              = "sso_config"
	UserSourceSSO             = "sso"
	DefaultSSOTokenPath       = "/oauth2/token"
	DefaultSSOUserInfo        = "/oauth2/userinfo"
	DefaultSSOAuthorizePath   = "/oauth2/authorize"
	SSOGrantPassword          = "password"
	SSOGrantAuthorizationCode = "authorization_code"
)

// SsoConfig Restosuite SSO / OAuth2 外部接入。
type SsoConfig struct {
	Enabled          bool   `json:"enabled"`
	GrantMode        string `json:"grant_mode"`   // password | authorization_code
	BaseURL          string `json:"base_url"`     // SSO 地址：IdP 服务根地址
	CallbackURL      string `json:"callback_url"` // SSO 回调地址：本平台在 IdP 登记的 redirect_uri
	AuthorizePath    string `json:"authorize_path"`
	TokenPath        string `json:"token_path"`
	UserInfoPath     string `json:"userinfo_path"`
	JWKSURI          string `json:"jwks_uri"` // JWKS 地址：校验 id_token / JWT，相对 SSO 地址或完整 URL
	ClientID         string `json:"client_id"`
	ClientSecret     string `json:"-"`
	ClientSecretEnc  string `json:"client_secret_enc,omitempty"`
	Scope            string `json:"scope"`
	UsernameClaim    string `json:"username_claim"`
	DisplayNameClaim string `json:"display_name_claim"`
	EmailClaim       string `json:"email_claim"`
}

func NormalizeSSOGrantMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case SSOGrantPassword, "pwd", "resource_owner":
		return SSOGrantPassword
	default:
		return SSOGrantAuthorizationCode
	}
}

func DefaultSsoConfig() *SsoConfig {
	return &SsoConfig{
		GrantMode:        SSOGrantAuthorizationCode,
		AuthorizePath:    DefaultSSOAuthorizePath,
		TokenPath:        DefaultSSOTokenPath,
		UserInfoPath:     DefaultSSOUserInfo,
		UsernameClaim:    "nickname",
		DisplayNameClaim: "name",
		EmailClaim:       "email",
	}
}

func (d *DB) GetSsoConfig(encKey string) (*SsoConfig, error) {
	v, err := d.GetGlobalArg(KeySsoConfig)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(v) == "" {
		return DefaultSsoConfig(), nil
	}
	var c SsoConfig
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return DefaultSsoConfig(), nil
	}
	if strings.TrimSpace(c.TokenPath) == "" {
		c.TokenPath = DefaultSSOTokenPath
	}
	if strings.TrimSpace(c.UserInfoPath) == "" {
		c.UserInfoPath = DefaultSSOUserInfo
	}
	if strings.TrimSpace(c.AuthorizePath) == "" {
		c.AuthorizePath = DefaultSSOAuthorizePath
	}
	c.GrantMode = NormalizeSSOGrantMode(c.GrantMode)
	if strings.TrimSpace(c.UsernameClaim) == "" {
		c.UsernameClaim = "nickname"
	}
	if strings.TrimSpace(c.DisplayNameClaim) == "" {
		c.DisplayNameClaim = "name"
	}
	if strings.TrimSpace(c.EmailClaim) == "" {
		c.EmailClaim = "email"
	}
	if c.ClientSecretEnc == "" {
		return &c, nil
	}
	if encKey != "" {
		plain, decErr := secret.Decrypt(encKey, c.ClientSecretEnc)
		if decErr != nil {
			return &c, fmt.Errorf("SSO Client Secret 解密失败，请到认证配置页重新保存")
		}
		c.ClientSecret = plain
	} else {
		c.ClientSecret = c.ClientSecretEnc
	}
	return &c, nil
}

func (d *DB) SetSsoConfig(encKey string, c *SsoConfig) error {
	if c == nil {
		return d.SetGlobalArg(KeySsoConfig, "", "SSO 登录配置")
	}
	enc := c.ClientSecretEnc
	if c.ClientSecret != "" {
		if encKey != "" {
			var err error
			enc, err = secret.Encrypt(encKey, c.ClientSecret)
			if err != nil {
				return err
			}
		} else {
			enc = c.ClientSecret
		}
	}
	tokenPath := strings.TrimSpace(c.TokenPath)
	if tokenPath == "" {
		tokenPath = DefaultSSOTokenPath
	}
	userInfo := strings.TrimSpace(c.UserInfoPath)
	if userInfo == "" {
		userInfo = DefaultSSOUserInfo
	}
	authorizePath := strings.TrimSpace(c.AuthorizePath)
	if authorizePath == "" {
		authorizePath = DefaultSSOAuthorizePath
	}
	toStore := SsoConfig{
		Enabled:          c.Enabled,
		GrantMode:        NormalizeSSOGrantMode(c.GrantMode),
		BaseURL:          strings.TrimRight(strings.TrimSpace(c.BaseURL), "/"),
		CallbackURL:      strings.TrimSpace(c.CallbackURL),
		AuthorizePath:    authorizePath,
		TokenPath:        tokenPath,
		UserInfoPath:     userInfo,
		JWKSURI:          strings.TrimSpace(c.JWKSURI),
		ClientID:         strings.TrimSpace(c.ClientID),
		ClientSecretEnc:  enc,
		Scope:            strings.TrimSpace(c.Scope),
		UsernameClaim:    strings.TrimSpace(c.UsernameClaim),
		DisplayNameClaim: strings.TrimSpace(c.DisplayNameClaim),
		EmailClaim:       strings.TrimSpace(c.EmailClaim),
	}
	if toStore.UsernameClaim == "" {
		toStore.UsernameClaim = "nickname"
	}
	if toStore.DisplayNameClaim == "" {
		toStore.DisplayNameClaim = "name"
	}
	if toStore.EmailClaim == "" {
		toStore.EmailClaim = "email"
	}
	raw, err := json.Marshal(toStore)
	if err != nil {
		return err
	}
	return d.SetGlobalArg(KeySsoConfig, string(raw), "SSO 登录配置（Client Secret 加密存储）")
}

func (c *SsoConfig) Public() map[string]any {
	if c == nil {
		c = DefaultSsoConfig()
	}
	return map[string]any{
		"enabled":            c.Enabled,
		"grant_mode":         NormalizeSSOGrantMode(c.GrantMode),
		"base_url":           c.BaseURL,
		"callback_url":       c.CallbackURL,
		"authorize_path":     c.AuthorizePath,
		"token_path":         c.TokenPath,
		"userinfo_path":      c.UserInfoPath,
		"jwks_uri":           c.JWKSURI,
		"client_id":          c.ClientID,
		"scope":              c.Scope,
		"username_claim":     c.UsernameClaim,
		"display_name_claim": c.DisplayNameClaim,
		"email_claim":        c.EmailClaim,
		"client_secret_set":  strings.TrimSpace(c.ClientSecret) != "" || strings.TrimSpace(c.ClientSecretEnc) != "",
	}
}

func (c *SsoConfig) UsePasswordGrant() bool {
	return c != nil && NormalizeSSOGrantMode(c.GrantMode) == SSOGrantPassword
}

func (c *SsoConfig) UseAuthorizeGrant() bool {
	return c != nil && NormalizeSSOGrantMode(c.GrantMode) == SSOGrantAuthorizationCode
}

func (c *SsoConfig) joinURL(pathOrURL string) string {
	p := strings.TrimSpace(pathOrURL)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

func (c *SsoConfig) AuthorizeURL() string {
	if c == nil {
		return ""
	}
	return c.joinURL(c.AuthorizePath)
}

func (c *SsoConfig) TokenURL() string {
	if c == nil {
		return ""
	}
	return c.joinURL(c.TokenPath)
}

func (c *SsoConfig) UserInfoURL() string {
	if c == nil {
		return ""
	}
	return c.joinURL(c.UserInfoPath)
}

func (c *SsoConfig) JWKSURL() string {
	if c == nil {
		return ""
	}
	return c.joinURL(c.JWKSURI)
}
