package sessionstore

import (
	"testing"

	"auto-dba/internal/config"
)

func TestStoreDisabledNoOp(t *testing.T) {
	s := New(config.RedisConfig{Enabled: false}, 24)
	if s.Enabled() {
		t.Fatal("expected disabled")
	}
	if err := s.Create(t.Context(), Session{JTI: "a", UserID: "u1"}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Valid(t.Context(), "u1", "a")
	if err != nil || !ok {
		t.Fatalf("disabled Valid should pass: ok=%v err=%v", ok, err)
	}
}

func TestDefaults(t *testing.T) {
	s := New(config.RedisConfig{Enabled: false, FailMode: ""}, 24)
	if s.FailOpen() {
		t.Fatal("empty fail_mode should not be open after applyDefaults on config; store keeps raw")
	}
}
