package cloudsvc

import (
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestClassifySlowPipeline(t *testing.T) {
	now := time.Date(2026, 11, 11, 6, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Minute).Format(time.RFC3339)
	old := now.Add(-40 * time.Minute).Format(time.RFC3339)

	cases := []struct {
		name   string
		cur    store.CloudCursor
		status string
	}{
		{name: "ok", cur: store.CloudCursor{Status: "ok", CursorAt: fresh, LastCount: 3, IngestedCount: 3}, status: "ok"},
		{name: "gap", cur: store.CloudCursor{Status: "gap", CursorAt: fresh, LastCount: 1, IngestedCount: 1}, status: "lag"},
		{name: "behind", cur: store.CloudCursor{Status: "ok", CursorAt: old, LastCount: 1, IngestedCount: 1}, status: "lag"},
		{name: "short", cur: store.CloudCursor{Status: "ok", CursorAt: fresh, LastCount: 4, IngestedCount: 1}, status: "lag"},
		{name: "error", cur: store.CloudCursor{Status: "error", CursorAt: fresh, FailCount: 0}, status: "failed"},
		{name: "fail count", cur: store.CloudCursor{Status: "ok", CursorAt: fresh, FailCount: 2, LastCount: 1, IngestedCount: 1}, status: "failed"},
	}
	for _, tc := range cases {
		got, _ := classifySlowPipeline(&tc.cur, now)
		if got != tc.status {
			t.Fatalf("%s: got %s want %s", tc.name, got, tc.status)
		}
	}
}

func TestBuildSlowPipelineUsesStoredHour(t *testing.T) {
	now := time.Date(2026, 11, 11, 6, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Minute).Format(time.RFC3339)
	cur := &store.CloudCursor{Status: "none", CursorAt: fresh, ProducedAt: fresh, LastCount: 0, IngestedCount: 0}
	item := buildSlowPipelineItem(store.Instance{ID: "i", Name: "pg", CloudInstanceID: "c"}, cur, true, now, 12)
	if item.Collected != 12 || item.Produced != 12 || item.Ingested != 12 || item.Lag != 0 || item.DelaySec != 0 {
		t.Fatalf("%+v", item)
	}
}

func TestBuildSlowPipelineNone(t *testing.T) {
	item := buildSlowPipelineItem(store.Instance{ID: "i", Name: "pg", CloudInstanceID: "c"}, nil, false, time.Now(), 4)
	if item.Status != "none" || item.StatusText != "未同步" || item.Kafka || item.Collected != 0 {
		t.Fatalf("%+v", item)
	}
}
