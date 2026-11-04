package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

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

func TestComputeCDCOccupancyLivePublication(t *testing.T) {
	init := &store.CDCTask{ID: "p1", Name: "p1", Status: store.StatusRunning,
		Description: encodeCDCExtraJSON("", map[string]any{"tables": []string{"a", "t_x"}})}
	children := []store.CDCTask{
		{ID: "c-pending", Name: "pend", Status: store.StatusPending, ApprovalStatus: store.StatusPending,
			Description: encodeCDCExtraJSON("", map[string]any{"tables": []string{"p_1"}})},
		{ID: "c-failed", Name: "fail", Status: store.StatusFailed,
			Description: encodeCDCExtraJSON("", map[string]any{"tables": []string{"t_a_0", "t_a_1"}})},
		{ID: "c-done", Name: "done", Status: store.StatusRunning,
			Description: encodeCDCExtraJSON("", map[string]any{"tables": []string{"b"}, "pipeline_status": "running"})},
		{ID: "c-rej", Name: "rej", Status: store.StatusRejected,
			Description: encodeCDCExtraJSON("", map[string]any{"tables": []string{"r"}})},
	}
	live := map[string]struct{}{"a": {}, "b": {}}

	occ := computeCDCOccupancy(init, children, live, true)
	got := map[string]bool{}
	for _, n := range occ.Tables {
		got[n] = true
	}
	if !got["a"] || !got["b"] || !got["p_1"] {
		t.Fatalf("occupied = %v", occ.Tables)
	}
	if got["t_x"] || got["t_a_0"] || got["r"] {
		t.Fatalf("stale/rejected must not occupy: %v", occ.Tables)
	}
	if len(occ.InFlight) != 1 || occ.InFlight[0].ByTaskID != "c-pending" {
		t.Fatalf("in_flight = %+v", occ.InFlight)
	}
	staleBy := map[string]string{}
	for _, e := range occ.Stale {
		staleBy[e.Table] = e.ByTaskID
	}
	if staleBy["t_x"] != "p1" || staleBy["t_a_0"] != "c-failed" || staleBy["t_a_1"] != "c-failed" || len(staleBy) != 3 {
		t.Fatalf("stale = %+v", occ.Stale)
	}

	fallback := computeCDCOccupancy(init, children, nil, false)
	want := []string{"a", "t_x", "p_1", "t_a_0", "t_a_1", "b"}
	if len(fallback.Tables) != len(want) || len(fallback.Stale) != 0 {
		t.Fatalf("fallback = %+v", fallback)
	}
	for i := range want {
		if fallback.Tables[i] != want[i] {
			t.Fatalf("fallback order = %v", fallback.Tables)
		}
	}
}

func TestCdcChildInFlight(t *testing.T) {
	cases := []struct {
		t    store.CDCTask
		want bool
	}{
		{store.CDCTask{Status: store.StatusPending}, true},
		{store.CDCTask{Status: store.StatusApproved, ApprovalStatus: store.StatusApproved}, true},
		{store.CDCTask{Status: store.StatusRunning}, true},
		{store.CDCTask{Status: store.StatusRunning, Description: encodeCDCExtraJSON("", map[string]any{"pipeline_status": "running"})}, false},
		{store.CDCTask{Status: store.StatusFailed}, false},
		{store.CDCTask{Status: "error"}, false},
	}
	for i, c := range cases {
		if got := cdcChildInFlight(&c.t); got != c.want {
			t.Fatalf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func TestParentTableStateSplitAddTables(t *testing.T) {
	st := cdcParentTableState{Occupied: []string{"a"}, Existing: []string{"a", "b"}, ExistsChecked: true}
	kept, occ, missing := st.splitAddTables([]string{"a", "b", "c", "public.b"})
	if len(kept) != 2 || kept[0] != "b" || kept[1] != "public.b" {
		t.Fatalf("kept = %v", kept)
	}
	if len(occ) != 1 || occ[0] != "a" || len(missing) != 1 || missing[0] != "c" {
		t.Fatalf("occ=%v missing=%v", occ, missing)
	}
	unchecked := cdcParentTableState{Occupied: []string{"a"}}
	kept, _, missing = unchecked.splitAddTables([]string{"a", "c"})
	if len(kept) != 1 || kept[0] != "c" || len(missing) != 0 {
		t.Fatalf("unchecked kept=%v missing=%v", kept, missing)
	}
}

func TestResolveBatchWantTablesPrefersRaw(t *testing.T) {
	raw := map[string]any{"tables": []any{"a", "b"}}
	got := resolveBatchWantTables(raw, "")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %v", got)
	}
	desc := encodeCDCExtraJSON("", map[string]any{"tables": []string{"x", "y"}})
	got = resolveBatchWantTables(map[string]any{}, desc)
	if len(got) != 2 || got[0] != "x" {
		t.Fatalf("fallback got %v", got)
	}
	if len(resolveBatchWantTables(map[string]any{}, "")) != 0 {
		t.Fatal("empty should stay empty")
	}
}

func TestBatchAddSkipMessage(t *testing.T) {
	if batchAddSkipMessage(nil) != "所选表均已在该任务同步" {
		t.Fatal("occupied-only message")
	}
	if batchAddSkipMessage([]string{"t1"}) != "所选表均已同步或该库不存在" {
		t.Fatal("missing message")
	}
}

func TestCdcMetaRemoteGroupKeyMergesSameHost(t *testing.T) {
	instA := &store.Instance{ID: "i1", Host: "pg.bill.sea.example", Port: 5432, Username: "u"}
	instB := &store.Instance{ID: "i2", Host: "pg.bill.sea.example", Port: 5432, Username: "u"}
	initA := &store.CDCTask{ID: "t1", SourceDB: "db_bill_0"}
	initB := &store.CDCTask{ID: "t2", SourceDB: "db_bill_1"}
	k1 := cdcMetaRemoteGroupKey("exec1", initA, instA)
	k2 := cdcMetaRemoteGroupKey("exec1", initB, instB)
	if k1 != k2 {
		t.Fatalf("same host should merge: %q vs %q", k1, k2)
	}
	instC := &store.Instance{ID: "i3", Host: "other.example", Port: 5432, Username: "u"}
	k3 := cdcMetaRemoteGroupKey("exec1", initA, instC)
	if k1 == k3 {
		t.Fatal("different host must not merge")
	}
}

func TestBuildBatchAvailTableAggsMissingAndOccupied(t *testing.T) {
	snaps := []batchAvailParentSnap{
		{
			Ref:      cdcParentTableRef{ParentID: "p1", Name: "task-1", SourceDB: "db_bill_1"},
			TableSet: map[string]struct{}{"tbl_a": {}, "tbl_b": {}},
			AvailSet: map[string]struct{}{"tbl_a": {}, "tbl_b": {}},
		},
		{
			Ref:      cdcParentTableRef{ParentID: "p2", Name: "task-2", SourceDB: "db_bill_2"},
			TableSet: map[string]struct{}{"tbl_a": {}},
			AvailSet: map[string]struct{}{}, // tbl_a occupied
		},
		{
			Ref:      cdcParentTableRef{ParentID: "p3", Name: "task-3", SourceDB: "db_bill_3"},
			TableSet: map[string]struct{}{}, // no tables
			AvailSet: map[string]struct{}{},
		},
	}
	order, agg := buildBatchAvailTableAggs(snaps)
	if len(order) != 2 {
		t.Fatalf("order=%v", order)
	}
	a := agg["tbl_a"]
	if a == nil || a.Exists != 2 || a.Addable != 1 {
		t.Fatalf("tbl_a exists=%d addable=%d", a.Exists, a.Addable)
	}
	if len(a.MissingParentIDs) != 1 || a.MissingParentIDs[0] != "p3" {
		t.Fatalf("tbl_a missing=%v", a.MissingParentIDs)
	}
	if len(a.OccupiedParentIDs) != 1 || a.OccupiedParentIDs[0] != "p2" {
		t.Fatalf("tbl_a occupied=%v", a.OccupiedParentIDs)
	}
	b := agg["tbl_b"]
	if b == nil || b.Exists != 1 || b.Addable != 1 {
		t.Fatalf("tbl_b exists=%d addable=%d", b.Exists, b.Addable)
	}
	if len(b.MissingParentIDs) != 2 {
		t.Fatalf("tbl_b missing want 2 got %v", b.MissingParentIDs)
	}
}

func TestAvailableTablesBatchValidatesIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &CDCHandler{}
	ids := make([]string, cdcBatchAddTableMaxParents+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("p%d", i)
	}
	tooMany, _ := json.Marshal(map[string]any{"parent_ids": ids})
	for _, body := range []string{`{}`, `{"parent_ids":[" "]}`, string(tooMany)} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/cdc-sync/tasks/batch-add-table/available-tables", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.AvailableTablesBatch(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s code=%d resp=%s", body[:min(len(body), 40)], w.Code, w.Body.String())
		}
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
		"publication_name":        "ai_beijing_db_bill_29",
		"slot_name":               "slot_ai_beijing_db_bill_29",
		"connector_name":          "ai_beijing_db_bill_29",
		"topic_prefix":            "ai_beijing_db_bill_29",
		"connector_id":            "conn-1",
		"connect_base_url":        "http://dz:8083",
		"env":                     "beijing",
		"source_schema":           "public",
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
