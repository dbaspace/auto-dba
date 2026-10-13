package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestResolveInstanceHostPrefersDomain(t *testing.T) {
	inst := &store.Instance{
		Host:       "10.0.0.1",
		DomainName: "pg.legacy.example",
		Domains:    []string{"pg.a.example", "pg.b.example"},
	}
	if got := resolveInstanceHost(inst, "pg.b.example"); got != "pg.b.example" {
		t.Fatalf("preferred domain: got %q", got)
	}
	if got := resolveInstanceHost(inst, ""); got != "10.0.0.1" {
		t.Fatalf("fallback host: got %q", got)
	}
	inst.Host = ""
	if got := resolveInstanceHost(inst, ""); got != "pg.legacy.example" {
		t.Fatalf("fallback domain_name: got %q", got)
	}
}

func TestInstanceAndDatabaseContainsDomain(t *testing.T) {
	inst := &store.Instance{Domains: []string{"Pg.A.Example", "pg.b.example"}}
	if !instanceContainsDomain(inst, "pg.a.example") {
		t.Fatal("expected case-insensitive instance domain match")
	}
	if instanceContainsDomain(inst, "pg.c.example") {
		t.Fatal("unexpected domain match")
	}
	db := &store.Database{Domains: []string{"pg.b.example"}}
	if !databaseContainsDomain(db, "PG.B.EXAMPLE") {
		t.Fatal("expected case-insensitive database domain match")
	}
	if databaseContainsDomain(db, "pg.a.example") {
		t.Fatal("database should not match unbound domain")
	}
}

func TestDDLDomainForInstance(t *testing.T) {
	raw := `{"domain":"fallback.example","data_sources":[{"instance_id":"i1","domain":"pg.a.example"},{"instance_id":"i2","domain":"pg.b.example"}]}`
	if got := ddlDomainForInstance(raw, "i2", "issue.example"); got != "pg.b.example" {
		t.Fatalf("per-instance: got %q", got)
	}
	if got := ddlDomainForInstance(raw, "i9", "issue.example"); got != "fallback.example" {
		t.Fatalf("need_rollback domain: got %q", got)
	}
	if got := ddlDomainForInstance(`{}`, "i1", "issue.example"); got != "issue.example" {
		t.Fatalf("issue fallback: got %q", got)
	}
}

func TestDDLDataSourceIDsFromDataSources(t *testing.T) {
	raw := `{"data_sources":[{"instance_id":"a","domain":"d1"},{"instance_id":"b","domain":"d2"},{"instance_id":"a","domain":"d3"}]}`
	got := ddlDataSourceIDs(raw, "fb")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v", got)
	}
}
