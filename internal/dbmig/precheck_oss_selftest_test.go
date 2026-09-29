package dbmig

import (
	"context"
	"os"
	"strings"
	"testing"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

// TestPrecheckOSSUsesEnvSettings 确认 transfer_mode=oss 时按任务环境解析配置。
func TestPrecheckOSSUsesEnvSettings(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	env := "selftest-oss-env"
	if exist, _ := d.GetOSSSettingByEnv(env); exist != nil {
		_ = d.DeleteOSSSetting(exist.ID)
	}
	t.Cleanup(func() {
		if exist, _ := d.GetOSSSettingByEnv(env); exist != nil {
			_ = d.DeleteOSSSetting(exist.ID)
		}
	})

	task := &store.MigrationTask{
		DBType:       "postgres",
		Env:          env,
		TransferMode: "oss",
		SourceID:     "missing-src",
		TargetID:     "missing-tgt",
	}

	items := Precheck(context.Background(), d, "test-secret", task)
	foundFail := false
	for _, it := range items {
		if it.Item == "transfer_mode" && it.Status == "failed" {
			foundFail = true
			if !strings.Contains(it.Message, "环境") && !strings.Contains(it.Message, "配置中心") {
				t.Fatalf("expected env/config hint, got %q", it.Message)
			}
		}
	}
	if !foundFail {
		t.Fatalf("expected transfer_mode failed when env OSS empty, items=%+v", items)
	}

	enc, err := secret.Encrypt("change-me-in-production", "SK_SELFTEST")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreateOSSSetting(&store.OSSSetting{
		Environment:   env,
		CloudProduct:  store.OSSProductAliyunOSS,
		Endpoint:      "https://oss-cn-hangzhou.aliyuncs.com",
		Bucket:        "auto-dba-selftest",
		Prefix:        "migration",
		AccessKey:     "AK_SELFTEST",
		SecretEnc:     enc,
		RetentionDays: 7,
		ChunkSizeMB:   100,
	}); err != nil {
		t.Fatal(err)
	}
	if !d.ResolveMigrationOSSConfigured(env) {
		t.Fatal("expected configured for env")
	}
	items2 := Precheck(context.Background(), d, "test-secret", task)
	foundPass := false
	for _, it := range items2 {
		if it.Item == "transfer_mode" && it.Status == "passed" {
			foundPass = true
		}
	}
	if !foundPass {
		t.Fatalf("expected transfer_mode passed when env OSS configured, items=%+v", items2)
	}
}
