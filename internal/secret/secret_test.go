package secret

import (
	"strings"
	"testing"
)

func TestEncryptDecryptV2RoundTrip(t *testing.T) {
	SetDefaultKeyID("k1")
	SetPreviousSecrets(nil)
	enc, err := Encrypt("active-key", "hello-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "enc:v2:k1:") {
		t.Fatalf("want v2 prefix, got %q", enc)
	}
	plain, err := Decrypt("active-key", enc)
	if err != nil || plain != "hello-secret" {
		t.Fatalf("decrypt: %q %v", plain, err)
	}
}

func TestDecryptV1Legacy(t *testing.T) {
	SetPreviousSecrets(nil)
	// 用手写 seal 路径：临时用旧逻辑 — Encrypt 现只写 v2，故先造 v1
	b64, err := seal("legacy-jwt", "old-pass")
	if err != nil {
		t.Fatal(err)
	}
	v1 := prefixV1 + b64
	plain, err := Decrypt("legacy-jwt", v1)
	if err != nil || plain != "old-pass" {
		t.Fatalf("v1 decrypt: %q %v", plain, err)
	}
}

func TestDecryptWithPreviousKey(t *testing.T) {
	SetDefaultKeyID("k2")
	SetPreviousSecrets([]string{"old-key"})
	enc, err := Encrypt("old-key", "rotate-me")
	if err != nil {
		t.Fatal(err)
	}
	// 用新主密钥 + previous 应能解开旧密钥加密的密文
	plain, err := Decrypt("new-key", enc)
	if err != nil || plain != "rotate-me" {
		t.Fatalf("previous decrypt: %q %v", plain, err)
	}
}

func TestMergePreviousSecrets(t *testing.T) {
	SetPreviousSecrets([]string{"a"})
	MergePreviousSecrets([]string{"b", "a", ""})
	got := previousSecrets()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("merge previous: %#v", got)
	}
}

func TestDecryptPlainLegacy(t *testing.T) {
	SetPreviousSecrets(nil)
	plain, err := Decrypt("any", "not-encrypted")
	if err != nil || plain != "not-encrypted" {
		t.Fatalf("got %q %v", plain, err)
	}
}

func TestIsEncrypted(t *testing.T) {
	if !IsEncrypted("enc:v1:abc") || !IsEncrypted("enc:v2:k1:abc") {
		t.Fatal("expected encrypted")
	}
	if IsEncrypted("plain") {
		t.Fatal("plain should be false")
	}
}

func TestEncryptIdempotent(t *testing.T) {
	SetDefaultKeyID("k1")
	enc, err := Encrypt("k", "x")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Encrypt("k", enc)
	if err != nil || again != enc {
		t.Fatalf("idempotent: %q %v", again, err)
	}
}
