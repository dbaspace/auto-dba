package handler

import (
	"errors"
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestSSOAuthorizeGate_DecryptErrorStillAllowsWhenEnabled(t *testing.T) {
	cfg := &store.SsoConfig{Enabled: true, ClientSecretEnc: "deadbeef"}
	decErr := errors.New("SSO Client Secret 解密失败，请到认证配置页重新保存")
	if msg := ssoAuthorizeGate(cfg, decErr); msg != "" {
		t.Fatalf("authorize should proceed when enabled despite decrypt err, got %q", msg)
	}
}

func TestSSOAuthorizeGate_Disabled(t *testing.T) {
	if msg := ssoAuthorizeGate(&store.SsoConfig{Enabled: false}, nil); msg != "SSO 未启用" {
		t.Fatalf("got %q", msg)
	}
	if msg := ssoAuthorizeGate(nil, nil); msg != "SSO 未启用" {
		t.Fatalf("got %q", msg)
	}
}

func TestSSOAuthorizeGate_NilConfigWithError(t *testing.T) {
	msg := ssoAuthorizeGate(nil, errors.New("db down"))
	if msg == "" || msg == "SSO 未启用" {
		t.Fatalf("expected surfaced error, got %q", msg)
	}
}

func TestSSOExchangeGate_DecryptErrorNotMaskedAsDisabled(t *testing.T) {
	cfg := &store.SsoConfig{Enabled: true, ClientSecretEnc: "deadbeef"}
	decErr := errors.New("SSO Client Secret 解密失败，请到认证配置页重新保存")
	msg := ssoExchangeGate(cfg, decErr)
	if msg == "" || msg == "SSO 未启用" {
		t.Fatalf("expected decrypt hint, got %q", msg)
	}
	if !strings.Contains(msg, "重新保存") {
		t.Fatalf("expected re-save hint, got %q", msg)
	}
}

func TestSSOExchangeGate_CipherWithoutPlain(t *testing.T) {
	cfg := &store.SsoConfig{Enabled: true, ClientSecretEnc: "deadbeef"}
	msg := ssoExchangeGate(cfg, nil)
	if !strings.Contains(msg, "解密失败") {
		t.Fatalf("got %q", msg)
	}
}

func TestSSOExchangeGate_OK(t *testing.T) {
	cfg := &store.SsoConfig{Enabled: true, ClientSecret: "secret"}
	if msg := ssoExchangeGate(cfg, nil); msg != "" {
		t.Fatalf("got %q", msg)
	}
}

func TestSSOExchangeGate_PublicClientNoSecret(t *testing.T) {
	cfg := &store.SsoConfig{Enabled: true}
	if msg := ssoExchangeGate(cfg, nil); msg != "" {
		t.Fatalf("public client without secret should pass gate, got %q", msg)
	}
}
