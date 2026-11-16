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
	ok, renewed, err := s.Valid(t.Context(), "u1", "a")
	if err != nil || !ok || !renewed.IsZero() {
		t.Fatalf("disabled Valid should pass: ok=%v renewed=%v err=%v", ok, renewed, err)
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

func loadSession(t *testing.T, s *Store, userID, jti string) Session {
	t.Helper()
	raw, err := s.rdb.HGet(t.Context(), s.userKey(userID), jti).Result()
	if err != nil {
		t.Fatal(err)
	}
	var sess Session
	if err := json.Unmarshal([]byte(raw), &sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestValidSameDayDoesNotExtend(t *testing.T) {
	s, mr := newTestStore(t)
	ctx := t.Context()
	near := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	if err := s.Create(ctx, Session{
		JTI: "j1", UserID: "u1", Username: "admin", ExpireAt: near,
		OnlineAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	ok, renewed, err := s.Valid(ctx, "u1", "j1")
	if err != nil || !ok || !renewed.IsZero() {
		t.Fatalf("same day: ok=%v renewed=%v err=%v", ok, renewed, err)
	}
	sess := loadSession(t, s, "u1", "j1")
	if sess.ExpireAt != near {
		t.Fatalf("expire_at=%s want %s", sess.ExpireAt, near)
	}
	if ttl := mr.TTL(s.userKey("u1")); ttl < time.Hour || ttl > 2*time.Hour+time.Minute {
		t.Fatalf("key ttl=%s want about 2h", ttl)
	}
	if ok, renewed, err = s.Valid(ctx, "u1", "j1"); err != nil || !ok || !renewed.IsZero() {
		t.Fatalf("second visit: ok=%v renewed=%v err=%v", ok, renewed, err)
	}
	if again := loadSession(t, s, "u1", "j1"); again.ExpireAt != near {
		t.Fatalf("second expire_at=%s", again.ExpireAt)
	}
}

func TestValidExtendsWhenExtendDateIsYesterday(t *testing.T) {
	s, mr := newTestStore(t)
	ctx := t.Context()
	future := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	yesterday := time.Now().Add(-36 * time.Hour)
	if err := s.Create(ctx, Session{
		JTI: "j1", UserID: "u1", Username: "admin",
		ExpireAt: future, ExtendDate: ShanghaiDay(yesterday),
		OnlineAt: yesterday.UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	ok, renewed, err := s.Valid(ctx, "u1", "j1")
	if err != nil || !ok || renewed.IsZero() {
		t.Fatalf("extend: ok=%v renewed=%v err=%v", ok, renewed, err)
	}
	sess := loadSession(t, s, "u1", "j1")
	exp, err := time.Parse(time.RFC3339, sess.ExpireAt)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Before(time.Now().Add(23*time.Hour)) || exp.After(time.Now().Add(25*time.Hour)) {
		t.Fatalf("expire_at=%s want about now+24h", sess.ExpireAt)
	}
	if sess.ExtendDate != ShanghaiDay(time.Now()) {
		t.Fatalf("extend_date=%s", sess.ExtendDate)
	}
	if ttl := mr.TTL(s.userKey("u1")); ttl < 23*time.Hour || ttl > 24*time.Hour+time.Minute {
		t.Fatalf("key ttl=%s", ttl)
	}
	ok, renewed, err = s.Valid(ctx, "u1", "j1")
	if err != nil || !ok || !renewed.IsZero() {
		t.Fatalf("same day after extend: ok=%v renewed=%v err=%v", ok, renewed, err)
	}
	if again := loadSession(t, s, "u1", "j1"); again.ExpireAt != sess.ExpireAt {
		t.Fatalf("expire moved again: %s -> %s", sess.ExpireAt, again.ExpireAt)
	}
}

func TestCreateSameDayKeepsExpire(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	if err := s.Create(ctx, Session{JTI: "j1", UserID: "u1", Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	first := loadSession(t, s, "u1", "j1")
	if err := s.Create(ctx, Session{JTI: "j2", UserID: "u1", Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	second := loadSession(t, s, "u1", "j2")
	if second.ExpireAt != first.ExpireAt {
		t.Fatalf("second expire=%s want %s", second.ExpireAt, first.ExpireAt)
	}
	if _, err := s.rdb.HGet(ctx, s.userKey("u1"), "j1").Result(); !errors.Is(err, redis.Nil) {
		t.Fatalf("old jti should be replaced, err=%v", err)
	}
}

func TestCreateAfterExpiryStartsNewWindow(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if err := s.Create(ctx, Session{
		JTI: "j1", UserID: "u1", Username: "admin",
		ExpireAt: past, ExtendDate: ShanghaiDay(time.Now()),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, Session{JTI: "j2", UserID: "u1", Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	sess := loadSession(t, s, "u1", "j2")
	exp, err := time.Parse(time.RFC3339, sess.ExpireAt)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Before(time.Now().Add(23*time.Hour)) || exp.After(time.Now().Add(25*time.Hour)) {
		t.Fatalf("expire_at=%s want about now+24h", sess.ExpireAt)
	}
}

func TestValidExpiredDoesNotRenew(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := t.Context()
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if err := s.Create(ctx, Session{JTI: "j2", UserID: "u2", Username: "admin", ExpireAt: past}); err != nil {
		t.Fatal(err)
	}
	ok, renewed, err := s.Valid(ctx, "u2", "j2")
	if ok || !renewed.IsZero() || !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expired: ok=%v renewed=%v err=%v", ok, renewed, err)
	}
	_, err = s.rdb.HGet(ctx, s.userKey("u2"), "j2").Result()
	if !errors.Is(err, redis.Nil) {
		t.Fatalf("expired session should be revoked, err=%v", err)
	}
}
