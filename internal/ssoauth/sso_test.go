package ssoauth

import (
	"testing"

	"auto-dba/internal/store"
)

func TestTokenAndUserInfoURL(t *testing.T) {
	cfg := &store.SsoConfig{
		BaseURL:       "https://sso.example.com/",
		AuthorizePath: "/oauth2/authorize",
		TokenPath:     "/oauth2/token",
		UserInfoPath:  "oauth2/userinfo",
	}
	if got := cfg.AuthorizeURL(); got != "https://sso.example.com/oauth2/authorize" {
		t.Fatalf("AuthorizeURL=%s", got)
	}
	if got := cfg.TokenURL(); got != "https://sso.example.com/oauth2/token" {
		t.Fatalf("TokenURL=%s", got)
	}
	if got := cfg.UserInfoURL(); got != "https://sso.example.com/oauth2/userinfo" {
		t.Fatalf("UserInfoURL=%s", got)
	}
	cfg.TokenPath = "https://other.example.com/token"
	if got := cfg.TokenURL(); got != "https://other.example.com/token" {
		t.Fatalf("absolute TokenURL=%s", got)
	}
}

func TestUserFromMapClaims(t *testing.T) {
	cfg := store.DefaultSsoConfig()
	info := userFromMap(cfg, map[string]any{
		"nickname": "alice",
		"sub":      "452",
		"name":     "Alice Zhang",
		"email":    "alice@example.com",
	}, "fallback")
	if info.Username != "alice" || info.DisplayName != "Alice Zhang" || info.Email != "alice@example.com" || info.Subject != "452" {
		t.Fatalf("%+v", info)
	}
	// 无 nickname 时才用 sub
	info2 := userFromMap(cfg, map[string]any{
		"sub":   "452",
		"email": "a@b.com",
	}, "fallback")
	if info2.Username != "452" || info2.Subject != "452" {
		t.Fatalf("sub fallback %+v", info2)
	}
}
