package store

import (
	"strings"
	"testing"
)

func TestTruncateQueryAuditStatement(t *testing.T) {
	if TruncateQueryAuditStatement("  select 1  ") != "select 1" {
		t.Fatal("trim")
	}
	long := strings.Repeat("a", maxQueryAuditStatement+10)
	got := TruncateQueryAuditStatement(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want ellipsis, got len=%d", len(got))
	}
}

func TestBuildQueryAuditWhere(t *testing.T) {
	ok := true
	where, args := buildQueryAuditWhere(QueryAuditFilter{
		Username: "alice",
		Kind:     "kafka",
		Success:  &ok,
		Q:        "SELECT",
		DateFrom: "2026-01-01",
		DateTo:   "2026-12-31",
	})
	if !strings.Contains(where, "username LIKE") || !strings.Contains(where, "kind=?") {
		t.Fatalf("where=%s", where)
	}
	if len(args) != 8 {
		t.Fatalf("args=%d %#v", len(args), args)
	}
}
