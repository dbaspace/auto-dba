//go:build selftest

package flashback

import (
	"context"
	"os"
	"testing"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

// 用法：
//
//	go test -tags=selftest ./internal/flashback -run TestLiveDefaultHeapSelftest -count=1 -v \
//	  -args  （或设环境变量）
//
// 环境变量：
//
//	FLASHBACK_SELFTEST_INSTANCE / FLASHBACK_SELFTEST_DATABASE
//
// 默认：b12701d4-59e5-4da2-92f6-c795894b5d9b / db_dba_000
func TestLiveDefaultHeapSelftest(t *testing.T) {
	cfgPath := os.Getenv("AUTO_DBA_CONFIG")
	if cfgPath == "" {
		cfgPath = "config/config.yaml"
	}
	// 从仓库根运行时可找到；测试 cwd 可能是 package 目录
	candidates := []string{cfgPath, "../../config/config.yaml", "../../../config/config.yaml"}
	var cfg *config.Config
	var err error
	for _, p := range candidates {
		cfg, err = config.Load(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	db, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		t.Fatalf("open meta db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	SetRuntime(&Runtime{Store: db, Secret: cfg.App.JWTSecret, Cfg: cfg.Flashback})

	inst := os.Getenv("FLASHBACK_SELFTEST_INSTANCE")
	if inst == "" {
		inst = "b12701d4-59e5-4da2-92f6-c795894b5d9b"
	}
	database := os.Getenv("FLASHBACK_SELFTEST_DATABASE")
	if database == "" {
		database = "db_dba_000"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := New().Selftest(ctx, &FlashbackSelftestReq{InstanceID: inst, Database: database})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range out.Checks {
		t.Logf("%s ok=%v detail=%s", c.Name, c.OK, c.Detail)
	}
	if !out.OK {
		t.Fatalf("selftest failed table=%s", out.Table)
	}
}
