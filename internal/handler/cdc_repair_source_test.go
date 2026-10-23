package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestMergeInstanceSourceHostAppendsUnknownDomain(t *testing.T) {
	inst := &store.Instance{
		DomainName: "pg.old.example",
		Host:       "pg.old.example",
		Port:       5432,
		Domains:    []string{"pg.old.example"},
	}
	mergeInstanceSourceHost(inst, "pg.new.example", 5433)
	if inst.Host != "pg.new.example" || inst.Port != 5433 {
		t.Fatalf("host/port = %s:%d", inst.Host, inst.Port)
	}
	if inst.DomainName != "pg.old.example" {
		t.Fatalf("domain_name changed: %s", inst.DomainName)
	}
	found := false
	for _, d := range inst.Domains {
		if d == "pg.new.example" {
			found = true
		}
	}
	if !found {
		t.Fatalf("domains = %#v", inst.Domains)
	}
}

func TestMergeInstanceSourceHostKeepsExistingDomain(t *testing.T) {
	inst := &store.Instance{
		DomainName: "pg.old.example",
		Host:       "10.0.0.1",
		Port:       5432,
		Domains:    []string{"pg.old.example", "pg.new.example"},
	}
	mergeInstanceSourceHost(inst, "pg.new.example", 5432)
	if len(inst.Domains) != 2 {
		t.Fatalf("domains duplicated: %#v", inst.Domains)
	}
}
