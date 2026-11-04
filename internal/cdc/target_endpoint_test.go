package cdc

import (
	"testing"

	"auto-dba/internal/store"
)

func TestMatchInstanceByEndpointHostDomainDomains(t *testing.T) {
	list := []store.Instance{
		{ID: "a", Host: "10.0.0.1", DomainName: "dorisdb.supply.example", Port: 9030, DBType: "starrocks"},
		{ID: "b", Host: "other.example,alias.example", Port: 9030, DBType: "starrocks", Domains: []string{"doris.alias.example"}},
	}
	if got := MatchInstanceByEndpoint(list, "dorisdb.supply.example", 9030); got == nil || got.ID != "a" {
		t.Fatalf("domain match: %#v", got)
	}
	if got := MatchInstanceByEndpoint(list, "alias.example", 9030); got == nil || got.ID != "b" {
		t.Fatalf("comma host match: %#v", got)
	}
	if got := MatchInstanceByEndpoint(list, "doris.alias.example", 0); got == nil || got.ID != "b" {
		t.Fatalf("domains match: %#v", got)
	}
	if got := MatchInstanceByEndpoint(list, "dorisdb.supply.example", 9031); got != nil {
		t.Fatalf("port mismatch should miss, got %#v", got)
	}
	if got := MatchInstanceByEndpoint(list, "missing.example", 9030); got != nil {
		t.Fatalf("missing: %#v", got)
	}
}
