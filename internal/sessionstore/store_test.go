package sessionstore

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

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

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	s := New(config.RedisConfig{Enabled: true, KeyPrefix: "t:", SessionTTLHours: 24, SlidingExpire: false}, 24)
	s.rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = s.rdb.Close() })
	return s, mr
}

func TestValidSlidesExpireUntilDeadline(t *testing.T) {
	s, mr := newTestStore(t)
	ctx := t.Context()
	near := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	if err := s.Create(ctx, Session{
		JTI: "j1", UserID: "u1", Username: "admin", ExpireAt: near,
		OnlineAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Valid(ctx, "u1", "j1")
	if err != nil || !ok {
		t.Fatalf("valid session: ok=%v err=%v", ok, err)
	}
	var sess Session
	raw, err := s.rdb.HGet(ctx, s.userKey("u1"), "j1").Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &sess); err != nil {
		t.Fatal(err)
	}
	exp, err := time.Parse(time.RFC3339, sess.ExpireAt)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Before(time.Now().Add(23*time.Hour)) || exp.After(time.Now().Add(25*time.Hour)) {
		t.Fatalf("expire_at=%s want about now+24h", sess.ExpireAt)
	}
	if ttl := mr.TTL(s.userKey("u1")); ttl < 23*time.Hour || ttl > 24*time.Hour+time.Minute {
		t.Fatalf("key ttl=%s", ttl)
	}

	held := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	sess.ExpireAt = held
	b, err := json.Marshal(sess)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.rdb.HSet(ctx, s.userKey("u1"), "j1", string(b)).Err(); err != nil {
		t.Fatal(err)
	}
	if ok, err = s.Valid(ctx, "u1", "j1"); err != nil || !ok {
		t.Fatalf("throttled valid: ok=%v err=%v", ok, err)
	}
	raw, err = s.rdb.HGet(ctx, s.userKey("u1"), "j1").Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &sess); err != nil {
		t.Fatal(err)
	}
	if sess.ExpireAt != held {
		t.Fatalf("slide within 1 minute: expire_at=%s want %s", sess.ExpireAt, held)
	}
}

func TestValidExpiredDoesNotRenew(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if err := s.Create(ctx, Session{JTI: "j2", UserID: "u2", Username: "admin", ExpireAt: past}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.Valid(ctx, "u2", "j2")
	if ok || !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired: ok=%v err=%v", ok, err)
	}
	_, err = s.rdb.HGet(ctx, s.userKey("u2"), "j2").Result()
	if !errors.Is(err, redis.Nil) {
		t.Fatalf("expired session should be revoked, err=%v", err)
	}
}
