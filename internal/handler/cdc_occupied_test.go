package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestCdcOccupancyReleased(t *testing.T) {
	if !cdcOccupancyReleased(&store.CDCTask{Status: store.StatusRejected}) {
		t.Fatal("rejected should release")
	}
	if !cdcOccupancyReleased(&store.CDCTask{ApprovalStatus: store.StatusCanceled}) {
		t.Fatal("canceled approval should release")
	}
	if cdcOccupancyReleased(&store.CDCTask{Status: store.StatusPending, ApprovalStatus: store.StatusPending}) {
		t.Fatal("pending should occupy")
	}
	if cdcOccupancyReleased(&store.CDCTask{Status: store.StatusFailed}) {
		t.Fatal("failed/error should still occupy")
	}
}

func TestRewriteCDCTaskTablesFiltersMappings(t *testing.T) {
	desc := encodeCDCExtraJSON("", map[string]any{
		"tables": []string{"a", "b", "c"},
		"mappings": []any{
			map[string]any{"source_tables": []string{"a"}, "target_table": "a"},
			map[string]any{"source_tables": []string{"b"}, "target_table": "b"},
		},
	})
	out := rewriteCDCTaskTables(desc, []string{"b"})
	got := sourceTablesFromCDCDesc(out)
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestOverlayParentPipelineExtraOverridesWrongChildNames(t *testing.T) {
	// 批量加表表单串用了 db_bill_7 的链路名，创建 db_bill_29 子任务时应改写为父任务值。
	child := encodeCDCExtraJSON("add", map[string]any{
		"tables":           []string{"orders"},
		"publication_name": "ai_beijing_db_bill_7",
		"slot_name":        "slot_ai_beijing_db_bill_7",
		"connector_name":   "ai_beijing_db_bill_7",
		"topic_prefix":     "ai_beijing_db_bill_7",
		"env":              "wrong",
		"source_schema":    "public",
	})
	parent := encodeCDCExtraJSON("", map[string]any{
		"publication_name": "ai_beijing_db_bill_29",
		"slot_name":        "slot_ai_beijing_db_bill_29",
		"connector_name":   "ai_beijing_db_bill_29",
		"topic_prefix":     "ai_beijing_db_bill_29",
		"connector_id":     "conn-1",
		"connect_base_url": "http://dz:8083",
		"env":              "beijing",
		"source_schema":    "public",
		"kafka_bootstrap_servers": "kafka:9092",
	})
	out := overlayParentPipelineExtra(child, parent)
	extra := mustParseCDCExtra(out)
	if extra["publication_name"] != "ai_beijing_db_bill_29" {
		t.Fatalf("publication=%v", extra["publication_name"])
	}
	if extra["slot_name"] != "slot_ai_beijing_db_bill_29" {
		t.Fatalf("slot=%v", extra["slot_name"])
	}
	if extra["connector_name"] != "ai_beijing_db_bill_29" {
		t.Fatalf("connector=%v", extra["connector_name"])
	}
	if extra["topic_prefix"] != "ai_beijing_db_bill_29" {
		t.Fatalf("topic=%v", extra["topic_prefix"])
	}
	if extra["env"] != "beijing" {
		t.Fatalf("env=%v", extra["env"])
	}
	if extra["connector_id"] != "conn-1" {
		t.Fatalf("connector_id=%v", extra["connector_id"])
	}
	// tables 仍保留子任务自身的待加表
	tables, _ := extra["tables"].([]any)
	if len(tables) != 1 || tables[0] != "orders" {
		t.Fatalf("tables=%v", extra["tables"])
	}
}

func mustParseCDCExtra(desc string) map[string]any {
	_, extra := splitCDCDescMap(desc)
	if extra == nil {
		return map[string]any{}
	}
	return extra
}
