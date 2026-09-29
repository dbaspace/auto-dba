package store

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMigrationOSSRoundtripAffectsConfigured 验证配置中心 OSS 落库后，
// 迁移预检依赖的 GetMigrationSettings().OSS.Configured 能正确翻转；脱敏写回不覆盖。
func TestMigrationOSSRoundtripAffectsConfigured(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	before := d.GetMigrationSettings().OSS
	t.Cleanup(func() {
		_ = d.SaveMigrationOSS(before)
	})

	// clear
	_ = d.SetGlobalArg(ArgMigrationOSSEndpoint, "", "迁移 OSS 访问地址")
	_ = d.SetGlobalArg(ArgMigrationOSSBucket, "", "迁移 OSS 存储桶名称")
	_ = d.SetGlobalArg(ArgMigrationOSSAccessKey, "", "迁移 OSS AccessKey ID")
	_ = d.SetGlobalArg(ArgMigrationOSSSecretKey, "", "迁移 OSS AccessKey Secret")
	if d.GetMigrationSettings().OSS.Configured {
		t.Fatal("expected not configured after clear")
	}

	if err := d.SaveMigrationOSS(MigrationOSSSettings{
		Endpoint:      "https://oss-cn-hangzhou.aliyuncs.com",
		Bucket:        "auto-dba-selftest",
		Prefix:        "migration-selftest",
		AccessKey:     "SELFTEST_AK_ROUNDTRIP",
		SecretKey:     "SELFTEST_SK_ROUNDTRIP",
		RetentionDays: 7,
		ChunkSizeMB:   100,
	}); err != nil {
		t.Fatal(err)
	}
	got := d.GetMigrationSettings().OSS
	if !got.Configured {
		t.Fatalf("expected configured, got %+v", got)
	}
	if got.Endpoint != "https://oss-cn-hangzhou.aliyuncs.com" || got.Bucket != "auto-dba-selftest" {
		t.Fatalf("endpoint/bucket mismatch: %+v", got)
	}
	if got.AccessKey != "SELFTEST_AK_ROUNDTRIP" || got.SecretKey != "SELFTEST_SK_ROUNDTRIP" {
		t.Fatalf("secrets not persisted: ak=%q sk=%q", got.AccessKey, got.SecretKey)
	}

	// masked save must keep previous secrets
	if err := d.SaveMigrationOSS(MigrationOSSSettings{
		Endpoint:  "https://oss-cn-beijing.aliyuncs.com",
		Bucket:    "auto-dba-selftest-2",
		Prefix:    "migration-selftest",
		AccessKey: "********",
		SecretKey: "********",
	}); err != nil {
		t.Fatal(err)
	}
	got2 := d.GetMigrationSettings().OSS
	if got2.Endpoint != "https://oss-cn-beijing.aliyuncs.com" || got2.Bucket != "auto-dba-selftest-2" {
		t.Fatalf("non-secret fields not updated: %+v", got2)
	}
	if got2.AccessKey != "SELFTEST_AK_ROUNDTRIP" || got2.SecretKey != "SELFTEST_SK_ROUNDTRIP" {
		t.Fatalf("masked save overwrote secrets: ak=%q sk=%q", got2.AccessKey, got2.SecretKey)
	}
}

func TestMigrationOSSMenuSeeded(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	// ensureMigrationOSSMenu is called on server boot; call directly for selftest
	if err := d.ensureMigrationOSSMenu("2026-09-24T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	m, err := d.GetMenu("m-cfg-oss")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || m.Path != "/config/oss" || m.ParentID != "m-cfg-access" {
		t.Fatalf("menu not ready: %+v", m)
	}
}

// TestPutSettingsPartialJSON 用纯 JSON 形态确认前端分块保存契约（approval / oss / performance）。
func TestPutSettingsPartialJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type req struct {
		ApprovalEnabled *bool                   `json:"approval_enabled"`
		ApproverUserIDs *string                 `json:"approver_user_ids"`
		OSS             *MigrationOSSSettings   `json:"oss"`
		Performance     *MigrationPerfSettings  `json:"performance"`
	}
	cases := []struct {
		name string
		body string
		want func(r req) bool
	}{
		{"approval_only", `{"approval_enabled":true,"approver_user_ids":"admin,dba"}`, func(r req) bool {
			return r.ApprovalEnabled != nil && *r.ApprovalEnabled && r.ApproverUserIDs != nil && r.OSS == nil && r.Performance == nil
		}},
		{"oss_only", `{"oss":{"endpoint":"https://x","bucket":"b","access_key":"a","secret_key":"s"}}`, func(r req) bool {
			return r.OSS != nil && r.OSS.Bucket == "b" && r.ApprovalEnabled == nil && r.Performance == nil
		}},
		{"perf_only", `{"performance":{"max_concurrent_tasks":3,"table_parallelism":4,"batch_size":10000,"single_table_timeout_min":30,"default_speed_limit":0,"schedule_tolerance_min":10}}`, func(r req) bool {
			return r.Performance != nil && r.Performance.BatchSize == 10000 && r.OSS == nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPut, "/migration/settings", bytes.NewBufferString(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			var r req
			if err := c.ShouldBindJSON(&r); err != nil {
				t.Fatal(err)
			}
			if !tc.want(r) {
				b, _ := json.Marshal(r)
				t.Fatalf("unexpected bind: %s", b)
			}
		})
	}
}
