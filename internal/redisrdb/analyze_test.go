package redisrdb

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestKeyPrefix(t *testing.T) {
	cases := map[string]string{
		"user:1001:profile": "user:",
		"cache|foo":         "cache|",
		"plain":             "plain",
		"":                  "(empty)",
	}
	for in, want := range cases {
		if got := keyPrefix(in); got != want {
			t.Fatalf("keyPrefix(%q)=%q want %q", in, got, want)
		}
	}
}

func TestBuildAdviceHugeKey(t *testing.T) {
	rep := &Report{
		TotalKeys:   10,
		TotalBytes:  20 << 20,
		KeysWithTTL: 8,
		TopKeys: []KeyItem{
			{Key: "big", Type: "string", Bytes: 12 << 20},
		},
	}
	adv := buildAdvice(rep)
	found := false
	for _, a := range adv {
		if a.Code == "huge_key" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected huge_key advice, got %#v", adv)
	}
}

func TestAnalyzeFileWithRedisCLI(t *testing.T) {
	if _, err := exec.LookPath("redis-cli"); err != nil {
		t.Skip("redis-cli not available")
	}
	dir := t.TempDir()
	rdb := filepath.Join(dir, "dump.rdb")
	// 用 redis-cli --rdb 需要连着的服务器；改用 redis-server 临时实例更稳，但环境可能无。
	// 这里若无可用服务器则跳过。
	host := os.Getenv("REDIS_RDB_TEST_ADDR")
	if host == "" {
		host = "127.0.0.1:6379"
	}
	ping := exec.Command("redis-cli", "-u", "redis://"+host, "PING")
	if out, err := ping.CombinedOutput(); err != nil || string(out) != "PONG\n" {
		t.Skipf("redis not reachable at %s: %v %s", host, err, out)
	}
	_ = exec.Command("redis-cli", "-u", "redis://"+host, "FLUSHDB").Run()
	_ = exec.Command("redis-cli", "-u", "redis://"+host, "SET", "user:1", "hello").Run()
	_ = exec.Command("redis-cli", "-u", "redis://"+host, "SETEX", "cache:tmp", "60", "x").Run()
	_ = exec.Command("redis-cli", "-u", "redis://"+host, "HSET", "user:1:meta", "a", "1").Run()
	cmd := exec.Command("redis-cli", "-u", "redis://"+host, "--rdb", rdb)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dump rdb: %v %s", err, out)
	}
	rep, err := AnalyzeFile(rdb, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TotalKeys < 2 {
		t.Fatalf("expected keys, got %+v", rep)
	}
	if len(rep.ByType) == 0 || len(rep.TopKeys) == 0 {
		t.Fatalf("empty aggregates: %+v", rep)
	}
}
