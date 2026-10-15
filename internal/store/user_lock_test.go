package store

import (
	"testing"
	"time"
)

func TestUserExternalCooldownActiveAndStatus(t *testing.T) {
	u := &User{Username: "alice"}
	if u.ExternalCooldownActive() || u.ComputeStatus() != UserStatusActive {
		t.Fatal("empty cooldown")
	}
	u.ExternalCooldownUntil = time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	if !u.ExternalCooldownActive() {
		t.Fatal("expect active cooldown")
	}
	if u.ComputeStatus() != UserStatusLocked {
		t.Fatalf("status=%s want locked", u.ComputeStatus())
	}
	u.ExternalCooldownUntil = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if u.ExternalCooldownActive() {
		t.Fatal("expired cooldown")
	}
	if u.ComputeStatus() != UserStatusActive {
		t.Fatalf("status=%s want active after expiry", u.ComputeStatus())
	}
	u.FailedLoginCount = MaxFailedLogins
	if u.ComputeStatus() != UserStatusLocked {
		t.Fatal("platform lock")
	}
}
