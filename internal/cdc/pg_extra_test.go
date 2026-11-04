package cdc

import (
	"context"
	"strings"
	"testing"
)

func TestReplicaIdentityFullStmt(t *testing.T) {
	stmt, sch, tb := replicaIdentityFullStmt("public", "tbl_tax_0")
	if sch != "public" || tb != "tbl_tax_0" {
		t.Fatalf("split = %s.%s", sch, tb)
	}
	if stmt != `ALTER TABLE "public"."tbl_tax_0" REPLICA IDENTITY FULL` {
		t.Fatalf("stmt = %s", stmt)
	}
	stmt2, sch2, tb2 := replicaIdentityFullStmt("public", "other.tbl_order")
	if sch2 != "other" || tb2 != "tbl_order" {
		t.Fatalf("qualified split = %s.%s", sch2, tb2)
	}
	if !strings.Contains(stmt2, `"other"."tbl_order"`) || !strings.HasSuffix(stmt2, " REPLICA IDENTITY FULL") {
		t.Fatalf("qualified stmt = %s", stmt2)
	}
	if s, _, tb := replicaIdentityFullStmt("public", "  "); tb != "" || s != "" {
		t.Fatalf("empty table should skip, got %q %q", s, tb)
	}
}

func TestSetReplicaIdentityFullNilAndEmpty(t *testing.T) {
	if err := SetReplicaIdentityFull(context.Background(), nil, "public", nil); err != nil {
		t.Fatalf("empty tables: %v", err)
	}
	if err := SetReplicaIdentityFull(context.Background(), nil, "public", []string{"t1"}); err == nil || !strings.Contains(err.Error(), "db nil") {
		t.Fatalf("nil db: %v", err)
	}
}

func TestMissingFromTableSet(t *testing.T) {
	have := map[string]struct{}{
		"orders": {},
		"users":  {},
	}
	got := MissingFromTableSet(have, "public", []string{"orders", "missing_a", "Users", "missing_b", "missing_a"})
	if len(got) != 2 {
		t.Fatalf("got %v want 2 missing", got)
	}
	if got[0] != "public.missing_a" || got[1] != "public.missing_b" {
		t.Fatalf("got %v", got)
	}
}

func TestMissingFromTableSetEmpty(t *testing.T) {
	if got := MissingFromTableSet(nil, "public", nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if got := MissingFromTableSet(map[string]struct{}{}, "public", []string{"a"}); len(got) != 1 || got[0] != "public.a" {
		t.Fatalf("got %v", got)
	}
}
