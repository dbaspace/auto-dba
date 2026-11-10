package cloudsvc

import "testing"

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
