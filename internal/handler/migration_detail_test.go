package handler

import (
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestParseFlexibleTime(t *testing.T) {
	if _, ok := parseFlexibleTime(""); ok {
		t.Fatal("empty should fail")
	}
	if _, ok := parseFlexibleTime("2026-08-13 18:03:18"); !ok {
		t.Fatal("datetime should parse")
	}
	if _, ok := parseFlexibleTime("2026-08-13T18:03:18Z"); !ok {
		t.Fatal("rfc3339 should parse")
	}
}

func TestMergeMigrationTagsKeepsSelection(t *testing.T) {
	cur := map[string]any{"selection": map[string]any{"db_count": 1, "table_count": 4}, "keep": true}
	got := store.MergeMigrationTags(cur, map[string]any{"selection": map[string]any{"estimated_total_bytes": 10}})
	sel := got["selection"].(map[string]any)
	if sel["db_count"] != 1 || sel["estimated_total_bytes"] != 10 {
		t.Fatalf("%v", sel)
	}
	if got["keep"] != true {
		t.Fatal("lost keep")
	}
}

func TestBuildProgressCompleted(t *testing.T) {
	h := &MigrationHandler{}
	now := time.Now().UTC()
	t0 := &store.MigrationTask{
		ID: "x", Status: store.StatusCompleted,
		TotalObjects: 4, CompletedObjects: 4,
		TotalRows: 100, CompletedRows: 100,
		StartedAt:  now.Add(-10 * time.Second).Format(time.RFC3339),
		FinishedAt: now.Format(time.RFC3339),
	}
	p := h.buildProgress(t0)
	if p["percent_objects"] != 100.0 {
		t.Fatalf("percent=%v", p["percent_objects"])
	}
	if p["elapsed_seconds"].(int) <= 0 {
		t.Fatalf("elapsed=%v", p["elapsed_seconds"])
	}
}
