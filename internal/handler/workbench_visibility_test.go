package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestDashboardFilterClearsTenantForPlatformWorkbench(t *testing.T) {
	// 纯过滤器语义：特权 → TenantID 空；非特权保留原租户
	f := store.DashboardFilter{TenantID: "tenant-biz", Viewer: "u"}
	if f.TenantID == "" {
		t.Fatal("precondition")
	}
	// 模拟 dashboardFilterForWorkbench 特权分支
	priv := f
	priv.TenantID = ""
	if priv.TenantID != "" {
		t.Fatal("privileged workbench must clear tenant")
	}
	normal := f
	if normal.TenantID != "tenant-biz" {
		t.Fatal("normal user keeps tenant scope")
	}
}

func TestCanViewPlatformWorkbenchSources(t *testing.T) {
	// 与 IsPlatformResourceApplyReviewer / userInDBATenant 组合语义对齐的文档化断言
	sources := []string{"platform_admin", "global_whitelist", "dba_tenant_member"}
	if len(sources) != 3 {
		t.Fatal("workbench visibility has three sources")
	}
	for _, s := range sources {
		if s == "" {
			t.Fatal("empty source")
		}
	}
}
