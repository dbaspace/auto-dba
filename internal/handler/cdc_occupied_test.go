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
