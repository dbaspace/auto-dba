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

func TestBuildQueryAuditWhereWorkflowTicketName(t *testing.T) {
	where, args := buildQueryAuditWhere(QueryAuditFilter{Q: "DQL-Tcb714eea3773"})
	if strings.Count(where, "instance_name LIKE") != 2 {
		t.Fatalf("where=%s", where)
	}
	if len(args) != 6 || args[0] != "%DQL-Tcb714eea3773%" || args[3] != "%Tcb714eea3773%" {
		t.Fatalf("args=%#v", args)
	}
	_, plain := buildQueryAuditWhere(QueryAuditFilter{Q: "Tcb714eea3773"})
	if len(plain) != 3 || plain[0] != "%Tcb714eea3773%" {
		t.Fatalf("plain=%#v", plain)
	}
	_, other := buildQueryAuditWhere(QueryAuditFilter{Q: "SELECT-1"})
	if len(other) != 3 {
		t.Fatalf("other=%#v", other)
	}
}
