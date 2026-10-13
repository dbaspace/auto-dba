package config

import "testing"

func TestFieldCryptoFallbackAndOverride(t *testing.T) {
	onlyJWT := AppConfig{JWTSecret: "jwt-only"}
	if FieldCryptoFromApp(onlyJWT) != "jwt-only" {
		t.Fatalf("fallback: %q", FieldCryptoFromApp(onlyJWT))
	}
	both := AppConfig{JWTSecret: "jwt", CryptoSecret: "crypto"}
	if FieldCryptoFromApp(both) != "crypto" {
		t.Fatalf("override: %q", FieldCryptoFromApp(both))
	}
}

func TestDiffConfigCryptoSecret(t *testing.T) {
	old := &Config{App: AppConfig{JWTSecret: "j", CryptoSecret: "a"}}
	next := &Config{App: AppConfig{JWTSecret: "j", CryptoSecret: "b"}}
	d := DiffConfig(old, next)
	if !d.CryptoSecret || d.JWTSecret {
		t.Fatalf("diff=%+v", d)
	}
	prev := &Config{App: AppConfig{JWTSecret: "j", CryptoSecret: "a", CryptoSecretPrevious: "old"}}
	d2 := DiffConfig(old, prev)
	if !d2.CryptoSecret {
		t.Fatal("previous change should set CryptoSecret")
	}
}
