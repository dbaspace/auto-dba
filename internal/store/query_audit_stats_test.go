package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/config"
)

func TestQueryAuditStats(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("AUTO_DBA_TEST_DSN"))
	if dsn == "" {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			t.Skip("no test dsn")
		}
		cfg, err := config.Load(filepath.Join(filepath.Dir(file), "..", "..", "config", "config.yaml"))
		if err != nil || cfg == nil {
			t.Skip("no test dsn")
		}
		dsn = strings.TrimSpace(cfg.DB.DSN)
	}
	if dsn == "" {
		t.Skip("no test dsn")
	}
	db, err := Open("postgres", dsn)
	if err != nil {
		if strings.Contains(err.Error(), "must be owner") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	user := "qa-stats-" + uuid.NewString()
	now := time.Now()
	oldAt := now.AddDate(0, 0, -8).UTC().Format(time.RFC3339)
	todayAt := now.UTC().Format(time.RFC3339)
	type row struct {
		kind, username, name, statement, target, at string
		success, elapsed                            int64
	}
	rows := []row{
		{"db", user, "统计甲", "select 1", "pg", todayAt, 1, 10},
		{"db", user, "统计甲", "select slow", "pg", todayAt, 0, 1 << 62},
		{"redis", user + "-b", "统计乙", "get k", "redis", todayAt, 1, 20},
		{"db", user, "统计甲", "select old", "pg", oldAt, 1, 1<<62 - 1},
	}
	for _, row := range rows {
		if _, err := db.Exec(
			`INSERT INTO tbl_query_audits(id, kind, username, display_name, target, statement, success, elapsed_ms, created_at)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			uuid.NewString(), row.kind, row.username, row.name, row.target, row.statement, row.success, row.elapsed, row.at,
		); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM tbl_query_audits WHERE username=? OR username=?`, user, user+"-b")
	})

	before, err := db.QueryAuditStats(7)
	if err != nil {
		t.Fatal(err)
	}
	// before includes the rows just inserted. Take a snapshot by subtracting the three in-window rows we know.
	if before.Total < 3 || before.Success < 2 || before.Failed < 1 || before.Today < 3 {
		t.Fatalf("window stats %+v", before)
	}
	if len(before.Trend) != 7 {
		t.Fatalf("trend days %d", len(before.Trend))
	}
	last := before.Trend[len(before.Trend)-1]
	if last.Date != now.Format("2006-01-02") || last.Total < 3 || last.Failed < 1 {
		t.Fatalf("today trend %+v", last)
	}
	var dbN, redisN int
	for _, k := range before.ByKind {
		if k.Kind == "db" {
			dbN = k.Count
		}
		if k.Kind == "redis" {
			redisN = k.Count
		}
	}
	if dbN < 2 || redisN < 1 {
		t.Fatalf("kinds %+v", before.ByKind)
	}
	if len(before.SlowTop) == 0 || before.SlowTop[0].Statement != "select slow" || before.SlowTop[0].ElapsedMS != 1<<62 {
		t.Fatalf("slow %+v", before.SlowTop)
	}
	var saw bool
	var prev = int(^uint(0) >> 1)
	for _, u := range before.TopUsers {
		if u.Count > prev {
			t.Fatalf("top users not sorted %+v", before.TopUsers)
		}
		prev = u.Count
		if u.Username == user {
			saw = true
			if u.Count < 2 || u.DisplayName != "统计甲" {
				t.Fatalf("user %+v", u)
			}
		}
	}
	if !saw && (len(before.TopUsers) < 5 || before.TopUsers[len(before.TopUsers)-1].Count < 2) {
		t.Fatalf("user %s missing from top %+v", user, before.TopUsers)
	}
}
