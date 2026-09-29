package redisquery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"auto-dba/internal/store"
)

const defaultPort = 6379

type Options struct {
	Username string
	Password string
}

func Addr(inst *store.Instance) string {
	if inst == nil {
		return fmt.Sprintf("localhost:%d", defaultPort)
	}
	h := strings.TrimSpace(inst.Host)
	if h == "" {
		h = strings.TrimSpace(inst.DomainName)
	}
	port := inst.Port
	if port <= 0 {
		port = defaultPort
	}
	if h == "" {
		return fmt.Sprintf("localhost:%d", port)
	}
	if _, _, err := net.SplitHostPort(h); err == nil {
		return h
	}
	return fmt.Sprintf("%s:%d", h, port)
}

func ParseDBIndex(dbName string) (int, error) {
	s := strings.TrimSpace(strings.ToLower(dbName))
	if s == "" {
		return 0, nil
	}
	s = strings.TrimPrefix(s, "db")
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 15 {
		return 0, fmt.Errorf("无效的 Redis 库名，应为 db0–db15")
	}
	return n, nil
}

func newClient(inst *store.Instance, opt Options, db int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         Addr(inst),
		Username:     strings.TrimSpace(firstNonEmpty(opt.Username, inst.Username)),
		Password:     opt.Password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  8 * time.Second,
		WriteTimeout: 8 * time.Second,
	})
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func ping(ctx context.Context, client *redis.Client) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	return nil
}

func openReadyClient(ctx context.Context, inst *store.Instance, opt Options, db int) (*redis.Client, error) {
	if inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	client := newClient(inst, opt, db)
	if err := ping(ctx, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func Ping(ctx context.Context, inst *store.Instance, opt Options) error {
	client, err := openReadyClient(ctx, inst, opt, 0)
	if err != nil {
		return err
	}
	defer client.Close()
	return nil
}

// InfoSection 返回 INFO <section> 的 key→value 映射（只读探测用）
func InfoSection(ctx context.Context, inst *store.Instance, opt Options, section string) (map[string]string, error) {
	client, err := openReadyClient(ctx, inst, opt, 0)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	sec := strings.TrimSpace(section)
	var raw string
	if sec == "" {
		raw, err = client.Info(ctx).Result()
	} else {
		raw, err = client.Info(ctx, sec).Result()
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}
