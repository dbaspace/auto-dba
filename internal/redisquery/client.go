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
	// Host 可选连接主机覆盖（须为实例登记的 host/domain_name/domains 之一）。
	Host string
}

// InstanceHosts 返回实例允许的连接主机（host、domain_name、domains），去重且保序。
func InstanceHosts(inst *store.Instance) []string {
	if inst == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	add(inst.Host)
	add(inst.DomainName)
	for _, d := range inst.Domains {
		add(d)
	}
	return out
}

// ResolveHost 解析连接主机：domain 为空用默认（host 优先，否则 domain_name/domains[0]）；
// 非空则必须命中 InstanceHosts，防止任意地址 SSRF。
func ResolveHost(inst *store.Instance, domain string) (string, error) {
	if inst == nil {
		return "", fmt.Errorf("实例不存在")
	}
	domain = strings.TrimSpace(domain)
	allowed := InstanceHosts(inst)
	if domain == "" {
		if len(allowed) == 0 {
			return "", fmt.Errorf("实例未配置连接主机或域名")
		}
		return allowed[0], nil
	}
	want := strings.ToLower(domain)
	for _, h := range allowed {
		if strings.ToLower(h) == want {
			return h, nil
		}
	}
	return "", fmt.Errorf("域名不在该实例登记范围内")
}

func instancePort(inst *store.Instance) int {
	if inst == nil || inst.Port <= 0 {
		return defaultPort
	}
	return inst.Port
}

func formatAddr(host string, port int) string {
	host = strings.TrimSpace(host)
	if port <= 0 {
		port = defaultPort
	}
	if host == "" {
		return fmt.Sprintf("localhost:%d", port)
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func Addr(inst *store.Instance) string {
	if inst == nil {
		return formatAddr("", defaultPort)
	}
	h, err := ResolveHost(inst, "")
	if err != nil {
		return formatAddr("", instancePort(inst))
	}
	return formatAddr(h, instancePort(inst))
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
	host := strings.TrimSpace(opt.Host)
	if host == "" {
		host, _ = ResolveHost(inst, "")
	}
	return redis.NewClient(&redis.Options{
		Addr:         formatAddr(host, instancePort(inst)),
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
