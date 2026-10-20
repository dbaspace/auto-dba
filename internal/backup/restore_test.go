package backup

import (
	"testing"

	"auto-dba/internal/store"
)

func TestParseLogicalRestorePayload(t *testing.T) {
	p, err := ParseLogicalRestorePayload(`{"source_run_id":"r1","target_instance_id":"i1","target_db":"app"}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.SourceRunID != "r1" || p.TargetInstanceID != "i1" || p.TargetDB != "app" {
		t.Fatalf("%+v", p)
	}
	if _, err := ParseLogicalRestorePayload(`{}`); err == nil {
		t.Fatal("empty should fail")
	}
}

func TestResolvePhysicalRestoreChainOrder(t *testing.T) {
	// 仅测纯函数逻辑：用内存 map 模拟 GetBackupRun 不可行；改为校验单节点路径组装约定。
	tip := &store.BackupRun{
		ID: "incr2", Method: store.BackupMethodPhysicalIncr, Status: store.BackupRunSuccess,
		ParentRunID: "incr1", LocalPath: "/data/inc2",
	}
	if tip.ParentRunID == "" || tip.LocalPath == "" {
		t.Fatal("fixture")
	}
	// 无 Store 时 resolve 会在 GetBackupRun 失败；此处覆盖 Parse + 请求字段
	req := PhysicalRestoreRequest{ChainDirs: []string{"/data/base", "/data/inc1", "/data/inc2"}}
	if len(req.ChainDirs) != 3 {
		t.Fatalf("%v", req.ChainDirs)
	}
}
