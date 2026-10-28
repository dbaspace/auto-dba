package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestReviewerAllowedByUnion(t *testing.T) {
	if msg := reviewerAllowedByUnion("songxincong", [][]string{
		{"songxincong", "zongjianhui"},
		{"admin"},
	}); msg != "" {
		t.Fatalf("union should allow name on one list: %s", msg)
	}
	if msg := reviewerAllowedByUnion("nobody", [][]string{
		{"songxincong"},
		{"admin"},
	}); msg == "" {
		t.Fatal("expected reject when not on any list")
	}
	if msg := reviewerAllowedByUnion("admin", [][]string{nil, {}}); msg != "" {
		t.Fatalf("all empty should pass: %s", msg)
	}
}

// 分库多实例：仅 marketing01 有逻辑库名，其余按前缀解析后并集仍含三人，指定审核人可通过。
func TestShardMultiInstanceApproverUnionPass(t *testing.T) {
	recsByInst := map[string][]store.Database{
		"i1": {{InstanceID: "i1", DBName: "db_marketing_064", Approvers: []string{"songxincong", "zongjianhui", "admin"}}},
		"i2": {{InstanceID: "i2", DBName: "db_marketing_OSS", Approvers: []string{"songxincong", "zongjianhui", "admin"}}},
		"i3": {{InstanceID: "i3", DBName: "db_marketing_000", Approvers: []string{"songxincong", "zongjianhui", "admin"}}},
		"i4": {{InstanceID: "i4", DBName: "db_marketing_004", Approvers: []string{"songxincong", "zongjianhui", "admin"}}},
	}
	formDB := "db_marketing_064"
	ids := []string{"i1", "i2", "i3", "i4"}
	var lists [][]string
	for _, id := range ids {
		lookup := resolveDbNameForApprover(id, formDB, recsByInst[id], true, "db_marketing_0")
		if lookup == "" {
			t.Fatalf("instance %s: expected resolved db name", id)
		}
		var hit *store.Database
		for i := range recsByInst[id] {
			if recsByInst[id][i].DBName == lookup {
				hit = &recsByInst[id][i]
				break
			}
		}
		if hit == nil || len(hit.Approvers) != 3 {
			t.Fatalf("instance %s lookup=%s approvers=%v", id, lookup, hit)
		}
		lists = append(lists, hit.Approvers)
	}
	if msg := reviewerAllowedByUnion("songxincong", lists); msg != "" {
		t.Fatal(msg)
	}
	// 对比：若误用逻辑库名查非主库，会落到空库级名单；此处断言非主库解析结果不是逻辑库名
	for _, id := range []string{"i2", "i3", "i4"} {
		if got := resolveDbNameForApprover(id, formDB, recsByInst[id], true, "db_marketing_0"); got == formDB {
			t.Fatalf("%s should not keep logical db name, got %q", id, got)
		}
	}
}
