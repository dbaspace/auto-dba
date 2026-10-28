package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestDbNameFamilyPrefix(t *testing.T) {
	if got := dbNameFamilyPrefix("db_marketing_064"); got != "db_marketing_" {
		t.Fatalf("got %q", got)
	}
	if got := dbNameFamilyPrefix("db_marketing_0"); got != "db_marketing_" {
		t.Fatalf("got %q", got)
	}
	if got := dbNameFamilyPrefix("plain"); got != "plain" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveDbNameForApprover(t *testing.T) {
	records := []store.Database{
		{InstanceID: "i1", DBName: "db_marketing_064", Approvers: []string{"a", "b", "c"}},
		{InstanceID: "i2", DBName: "db_marketing_OSS", Approvers: []string{"a", "b", "c"}},
		{InstanceID: "i3", DBName: "db_marketing_000", Approvers: []string{"a", "b", "c"}},
		{InstanceID: "i3", DBName: "other_db", Approvers: nil},
	}
	if got := resolveDbNameForApprover("i1", "db_marketing_064", records, false, ""); got != "db_marketing_064" {
		t.Fatalf("exact got %q", got)
	}
	if got := resolveDbNameForApprover("i2", "db_marketing_064", records, true, "db_marketing_0"); got != "db_marketing_OSS" {
		t.Fatalf("family fallback got %q", got)
	}
	if got := resolveDbNameForApprover("i3", "db_marketing_064", records, true, "db_marketing_0"); got != "db_marketing_000" {
		t.Fatalf("prefix got %q", got)
	}
	if got := resolveDbNameForApprover("i2", "db_marketing_064", records, false, ""); got != "" {
		t.Fatalf("no shard should miss got %q", got)
	}
}
