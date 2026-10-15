package store

import "testing"

func TestSMTPConfiguredVsReady(t *testing.T) {
	cfg := &SMTPConfig{
		Enabled:     false,
		Host:        "smtp.example.com",
		Username:    "a@example.com",
		From:        "a@example.com",
		PasswordEnc: "x",
	}
	if !cfg.Configured() {
		t.Fatal("expected Configured")
	}
	if cfg.Ready() {
		t.Fatal("enabled=false should not Ready")
	}
	cfg.Enabled = true
	if !cfg.Ready() {
		t.Fatal("expected Ready when enabled")
	}
	empty := &SMTPConfig{Enabled: true, Host: "x"}
	if empty.Configured() {
		t.Fatal("missing addr/password should not Configured")
	}
}
