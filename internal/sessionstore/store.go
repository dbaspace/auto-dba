package sessionstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"auto-dba/internal/config"
)

// Session 登录会话（Redis Hash field value；不存 JWT 明文）。
type Session struct {
	JTI       string `json:"jti"`
	UserID    string `json:"user_id"`
	TenantID  string `json:"tenant_id,omitempty"`
	Username  string `json:"username"`
	IP        string `json:"ip,omitempty"`
	UA        string `json:"ua,omitempty"`
	OnlineAt  string `json:"online_at"`
	ExpireAt  string `json:"expire_at"`
	Kind      string `json:"kind,omitempty"` // normal | impersonate
	Status    string `json:"status,omitempty"` // 展示用：会话有效
}

// Store Redis 会话权威源；Enabled=false 时所有写/校验为 no-op，不影响现网。
type Store struct {
	mu     sync.RWMutex
	rdb    *redis.Client
	cfg    config.RedisConfig
	jwtTTL int // hours fallback
}

func New(cfg config.RedisConfig, jwtTTLHours int) *Store {
	s := &Store{jwtTTL: jwtTTLHours}
	s.apply(cfg, jwtTTLHours)
	return s
}

func (s *Store) apply(cfg config.RedisConfig, jwtTTLHours int) {
	if jwtTTLHours > 0 {
		s.jwtTTL = jwtTTLHours
	}
	s.cfg = cfg
	if strings.TrimSpace(s.cfg.KeyPrefix) == "" {
		s.cfg.KeyPrefix = "autodba:"
	}
	if !strings.HasSuffix(s.cfg.KeyPrefix, ":") {
		s.cfg.KeyPrefix += ":"
	}
}

// Reconfigure 热更：连接参数变化时重建客户端。
func (s *Store) Reconfigure(cfg config.RedisConfig, jwtTTLHours int, reconnect bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.rdb
	s.apply(cfg, jwtTTLHours)
	if !cfg.Enabled || strings.TrimSpace(cfg.Addr) == "" {
		s.rdb = nil
		if old != nil {
			_ = old.Close()
		}
		log.Printf("sessionstore: redis disabled")
		return
	}
	if !reconnect && s.rdb != nil {
		return
	}
	cli := redis.NewClient(&redis.Options{
		Addr:     strings.TrimSpace(cfg.Addr),
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err := cli.Ping(ctx).Err()
	cancel()
	if err != nil {
		_ = cli.Close()
		log.Printf("sessionstore: redis ping failed: %v (keep previous client)", err)
		return
	}
	s.rdb = cli
	if old != nil && old != cli {
		_ = old.Close()
	}
	log.Printf("sessionstore: redis connected addr=%s db=%d", cfg.Addr, cfg.DB)
}

func (s *Store) Enabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Enabled && s.rdb != nil
}

func (s *Store) FailOpen() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.EqualFold(s.cfg.FailMode, "open")
}

func (s *Store) MaxSessionsPerUser() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.MaxSessionsPerUser
}

func (s *Store) ReconcileInterval() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.cfg.ReconcileIntervalMin
	if m <= 0 {
		m = 5
	}
	return time.Duration(m) * time.Minute
}

func (s *Store) client() *redis.Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rdb
}

func (s *Store) prefix() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.KeyPrefix
}

func (s *Store) userKey(userID string) string {
	return s.prefix() + "user:" + userID
}

func (s *Store) onlineKey() string {
	return s.prefix() + "online:users"
}

func (s *Store) sessionTTL() time.Duration {
	s.mu.RLock()
	h := s.cfg.SessionTTLHours
	jwtH := s.jwtTTL
	s.mu.RUnlock()
	if h <= 0 {
		h = jwtH
	}
	if h <= 0 {
		h = 24
	}
	return time.Duration(h) * time.Hour
}

// Create 登录：先写 Redis。max_sessions=1 时清空旧会话。
func (s *Store) Create(ctx context.Context, sess Session) error {
	cli := s.client()
	if cli == nil {
		return nil
	}
	if sess.JTI == "" || sess.UserID == "" {
		return fmt.Errorf("session jti/user_id required")
	}
	if sess.Kind == "" {
		sess.Kind = "normal"
	}
	sess.Status = "会话有效"
	now := time.Now()
	if sess.OnlineAt == "" {
		sess.OnlineAt = now.UTC().Format(time.RFC3339)
	}
	ttl := s.sessionTTL()
	if sess.ExpireAt == "" {
		sess.ExpireAt = now.Add(ttl).UTC().Format(time.RFC3339)
	}
	uk := s.userKey(sess.UserID)
	if s.MaxSessionsPerUser() == 1 {
		_ = cli.Del(ctx, uk).Err()
	}
	b, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	pipe := cli.Pipeline()
	pipe.HSet(ctx, uk, sess.JTI, string(b))
	// key TTL = max expire among fields（简化：用本次会话 TTL 刷新）
	pipe.Expire(ctx, uk, ttl)
	score := float64(now.Unix())
	if t, e := time.Parse(time.RFC3339, sess.OnlineAt); e == nil {
		score = float64(t.Unix())
	}
	pipe.ZAdd(ctx, s.onlineKey(), redis.Z{Score: score, Member: sess.UserID})
	_, err = pipe.Exec(ctx)
	return err
}

// Valid 鉴权：HGET + expire_at。
func (s *Store) Valid(ctx context.Context, userID, jti string) (bool, error) {
	cli := s.client()
	if cli == nil {
		return true, nil
	}
	if userID == "" || jti == "" {
		return false, nil
	}
	raw, err := cli.HGet(ctx, s.userKey(userID), jti).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var sess Session
	if err := json.Unmarshal([]byte(raw), &sess); err != nil {
		return false, err
	}
	if exp, e := time.Parse(time.RFC3339, sess.ExpireAt); e == nil && time.Now().After(exp) {
		_ = s.RevokeJTI(ctx, userID, jti)
		return false, nil
	}
	s.mu.RLock()
	slide := s.cfg.SlidingExpire
	s.mu.RUnlock()
	if slide {
		_ = cli.Expire(ctx, s.userKey(userID), s.sessionTTL()).Err()
	}
	return true, nil
}

// RevokeJTI 踢单端 / 登出。
func (s *Store) RevokeJTI(ctx context.Context, userID, jti string) error {
	cli := s.client()
	if cli == nil {
		return nil
	}
	uk := s.userKey(userID)
	if err := cli.HDel(ctx, uk, jti).Err(); err != nil {
		return err
	}
	n, _ := cli.HLen(ctx, uk).Result()
	if n == 0 {
		_ = cli.Del(ctx, uk).Err()
		_ = cli.ZRem(ctx, s.onlineKey(), userID).Err()
	}
	return nil
}

// RevokeUser 踢用户全部会话。
func (s *Store) RevokeUser(ctx context.Context, userID string) error {
	cli := s.client()
	if cli == nil {
		return nil
	}
	pipe := cli.Pipeline()
	pipe.Del(ctx, s.userKey(userID))
	pipe.ZRem(ctx, s.onlineKey(), userID)
	_, err := pipe.Exec(ctx)
	return err
}

// ListOnlineUsers 分页在线用户（ZSET），并懒删孤儿。
func (s *Store) ListOnlineUsers(ctx context.Context, offset, limit int64) (userIDs []string, total int64, err error) {
	cli := s.client()
	if cli == nil {
		return nil, 0, nil
	}
	ok := s.onlineKey()
	total, err = cli.ZCard(ctx, ok).Result()
	if err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 20
	}
	ids, err := cli.ZRevRange(ctx, ok, offset, offset+limit-1).Result()
	if err != nil {
		return nil, 0, err
	}
	out := make([]string, 0, len(ids))
	for _, uid := range ids {
		exists, e := cli.Exists(ctx, s.userKey(uid)).Result()
		if e != nil {
			return nil, 0, e
		}
		if exists == 0 {
			_ = cli.ZRem(ctx, ok, uid).Err()
			total--
			continue
		}
		out = append(out, uid)
	}
	if total < 0 {
		total = 0
	}
	return out, total, nil
}

// SessionsOfUser 某用户全部会话 field。
func (s *Store) SessionsOfUser(ctx context.Context, userID string) ([]Session, error) {
	cli := s.client()
	if cli == nil {
		return nil, nil
	}
	m, err := cli.HGetAll(ctx, s.userKey(userID)).Result()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]Session, 0, len(m))
	for jti, raw := range m {
		var sess Session
		if json.Unmarshal([]byte(raw), &sess) != nil {
			continue
		}
		if sess.JTI == "" {
			sess.JTI = jti
		}
		if exp, e := time.Parse(time.RFC3339, sess.ExpireAt); e == nil && now.After(exp) {
			_ = s.RevokeJTI(ctx, userID, jti)
			continue
		}
		sess.Status = "会话有效"
		out = append(out, sess)
	}
	return out, nil
}

// CountOnline 有效用户数（ZCARD，读前可懒清）。
func (s *Store) CountOnline(ctx context.Context) (int64, error) {
	cli := s.client()
	if cli == nil {
		return 0, nil
	}
	return cli.ZCard(ctx, s.onlineKey()).Result()
}

// OnlineUserIDsAll 对账用：全部 ZSET member。
func (s *Store) OnlineUserIDsAll(ctx context.Context) ([]string, error) {
	cli := s.client()
	if cli == nil {
		return nil, nil
	}
	return cli.ZRange(ctx, s.onlineKey(), 0, -1).Result()
}

// UserKeyExists 对账。
func (s *Store) UserKeyExists(ctx context.Context, userID string) (bool, error) {
	cli := s.client()
	if cli == nil {
		return false, nil
	}
	n, err := cli.Exists(ctx, s.userKey(userID)).Result()
	return n > 0, err
}

// RemoveOnlineMember 对账清理 ZSET。
func (s *Store) RemoveOnlineMember(ctx context.Context, userID string) error {
	cli := s.client()
	if cli == nil {
		return nil
	}
	return cli.ZRem(ctx, s.onlineKey(), userID).Err()
}

// ParseUserIDFromZ 兼容数字/字符串 member。
func ParseUserIDFromZ(m string) string {
	return strings.TrimSpace(m)
}

// SessionTTLHours 供 handler 展示。
func (s *Store) SessionTTLHours() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.SessionTTLHours > 0 {
		return s.cfg.SessionTTLHours
	}
	return s.jwtTTL
}

// AddrHint 管理页提示。
func (s *Store) AddrHint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.cfg.Enabled {
		return "disabled"
	}
	return s.cfg.Addr + " db=" + strconv.Itoa(s.cfg.DB)
}
