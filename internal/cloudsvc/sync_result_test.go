package cloudsvc

import (
	"errors"
	"testing"

	"auto-dba/internal/cloud"
	"auto-dba/internal/store"
)

func TestMatchAllTenants(t *testing.T) {
	if !matchAllTenants("tenant-default") {
		t.Fatal("default tenant cloud account matches instances in every tenant")
	}
	if matchAllTenants("tenant-sales") || matchAllTenants("") {
		t.Fatal("other tenants stay inside their own instances")
	}
}

func TestFinalizeSyncResult(t *testing.T) {
	if err := finalizeSyncResult(&SyncResult{Synced: 1, Skipped: []string{"迁移-tencent 缺少只读密钥"}}); err != nil {
		t.Fatalf("有成功时应忽略跳过: %v", err)
	}
	if err := finalizeSyncResult(&SyncResult{Synced: 0, Skipped: []string{"迁移-tencent 缺少只读密钥"}}); err != nil {
		t.Fatalf("仅跳过不应失败: %v", err)
	}
	if err := finalizeSyncResult(&SyncResult{Synced: 0, Errors: []string{"resto/mysql: timeout"}}); err == nil {
		t.Fatal("无成功且有硬错误应失败")
	}
	if err := finalizeSyncResult(&SyncResult{Synced: 2, Errors: []string{"x"}}); err != nil {
		t.Fatalf("部分成功不因其它错误整单失败: %v", err)
	}
}

func TestClassifySyncErrKeepsLastSuccess(t *testing.T) {
	status, msg, keep, fail := classifySyncErr(errNotScheduled)
	if status != "skipped" || msg != "未勾选定时同步" || !keep || fail {
		t.Fatalf("skipped = %s %s keep=%v fail=%v", status, msg, keep, fail)
	}
	status, msg, keep, fail = classifySyncErr(errSyncBusy)
	if status != "running" || msg != "上一轮仍在执行" || !keep || fail {
		t.Fatalf("busy = %s %s keep=%v fail=%v", status, msg, keep, fail)
	}
	status, _, keep, fail = classifySyncErr(errors.New("timeout"))
	if status != "error" || !keep || !fail {
		t.Fatalf("timeout = %s keep=%v fail=%v", status, keep, fail)
	}
	status, _, keep, fail = classifySyncErr(nil)
	if status != "ok" || keep || fail {
		t.Fatalf("ok = %s keep=%v fail=%v", status, keep, fail)
	}
}

func TestUnscheduledEngines(t *testing.T) {
	acc := &store.CloudAccount{Vendor: cloud.VendorTencent, SyncEngines: []string{"postgres"}}
	got := unscheduledEngines(acc, []string{cloud.EnginePostgres, cloud.EngineRedis})
	if len(got) != 1 || got[0] != cloud.EngineRedis {
		t.Fatalf("unchecked = %v", got)
	}
	acc.SyncEngines = nil
	got = unscheduledEngines(acc, []string{cloud.EnginePostgres, cloud.EngineRedis})
	if len(got) != 2 {
		t.Fatalf("none checked = %v", got)
	}
}
