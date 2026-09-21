package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQueryAuditStats(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "t.db"), "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)

	rows := []*QueryAudit{
		{Kind: "db", Username: "u1", DisplayName: "用户1", Statement: "select 1", Success: true, ElapsedMS: 100, CreatedAt: today.Format(time.RFC3339)},
		{Kind: "db", Username: "u1", DisplayName: "用户1", Statement: "select 2", Success: false, ElapsedMS: 500, CreatedAt: today.Format(time.RFC3339)},
		{Kind: "redis", Username: "u2", Statement: "GET a", Success: true, ElapsedMS: 20, CreatedAt: yesterday.Format(time.RFC3339)},
		{Kind: "kafka", Username: "u2", Statement: "topic=t", Success: true, ElapsedMS: 80, CreatedAt: yesterday.Format(time.RFC3339)},
	}
	for _, r := range rows {
		if err := db.CreateQueryAudit(r); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := db.QueryAuditStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Days != 7 {
		t.Fatalf("days=%d", stats.Days)
	}
	if stats.Total != 4 || stats.Success != 3 || stats.Failed != 1 {
		t.Fatalf("totals %+v", stats)
	}
	if stats.Today != 2 {
		t.Fatalf("today=%d", stats.Today)
	}
	if len(stats.Trend) != 7 {
		t.Fatalf("trend len=%d", len(stats.Trend))
	}
	if len(stats.ByKind) < 2 {
		t.Fatalf("by_kind=%+v", stats.ByKind)
	}
	if len(stats.TopUsers) < 1 || stats.TopUsers[0].Count < 1 {
		t.Fatalf("top_users=%+v", stats.TopUsers)
	}
	if len(stats.SlowTop) < 1 || stats.SlowTop[0].ElapsedMS < stats.SlowTop[len(stats.SlowTop)-1].ElapsedMS {
		// first should be slowest
		if len(stats.SlowTop) > 0 && stats.SlowTop[0].ElapsedMS != 500 {
			t.Fatalf("slow_top=%+v", stats.SlowTop)
		}
	}
}
