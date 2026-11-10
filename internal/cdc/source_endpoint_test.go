package cdc

import (
	"testing"

	"auto-dba/internal/store"
)

func TestEffectiveSourceEndpointPriority(t *testing.T) {
	inst := &store.Instance{Host: "pg.member.example", DomainName: "pg.member.example", Port: 5432}
	cfg := `{"database.hostname":"pg.dbz.example","database.port":"6432"}`
	cases := []struct {
		name  string
		extra map[string]string
		host  string
		port  int
	}{
		{"task source_host", map[string]string{"source_host": "pg.bill.example", "source_port": "5433", "debezium_config": cfg}, "pg.bill.example", 5433},
		{"task host without port", map[string]string{"source_host": "pg.bill.example"}, "pg.bill.example", 5432},
		{"debezium config", map[string]string{"debezium_config": cfg}, "pg.dbz.example", 6432},
		{"instance fallback", map[string]string{}, "pg.member.example", 5432},
		{"nil extra", nil, "pg.member.example", 5432},
	}
	for _, c := range cases {
		host, port := EffectiveSourceEndpoint(c.extra, inst)
		if host != c.host || port != c.port {
			t.Fatalf("%s: got %s:%d want %s:%d", c.name, host, port, c.host, c.port)
		}
	}
}

func TestTaskSourceInstanceUsesEffectiveEndpoint(t *testing.T) {
	inst := &store.Instance{ID: "i1", Host: "pg.member.example", Port: 5432, Username: "u"}
	got := TaskSourceInstance(inst, map[string]string{"source_host": "pg.bill.example", "source_port": "5433"})
	if got.Host != "pg.bill.example" || got.Port != 5433 || got.Username != "u" {
		t.Fatalf("override = %#v", got)
	}
	if inst.Host != "pg.member.example" || inst.Port != 5432 {
		t.Fatalf("original mutated: %#v", inst)
	}
	fromDz := TaskSourceInstance(inst, map[string]string{"debezium_config": `{"database.hostname":"pg.dbz.example","database.port":"6432"}`})
	if fromDz.Host != "pg.dbz.example" || fromDz.Port != 6432 {
		t.Fatalf("debezium hostname should override, got %s:%d", fromDz.Host, fromDz.Port)
	}
	same := TaskSourceInstance(inst, map[string]string{})
	if same.Host != "pg.member.example" {
		t.Fatalf("empty extra keeps instance host, got %s", same.Host)
	}
	if TaskSourceInstance(nil, nil) != nil {
		t.Fatal("nil inst")
	}
}

func TestBuildConnectorPlansUsesTaskSourceHost(t *testing.T) {
	inst := &store.Instance{Host: "pg.member.example", Port: 5432}
	extra := map[string]string{"source_host": "pg.bill.example", "source_port": "5432", "publication_name": "pub_a", "slot_name": "slot_a", "connector_name": "conn_a", "topic_prefix": "tp", "tables": "t1"}
	host, port := EffectiveSourceEndpoint(extra, inst)
	plans, err := buildConnectorPlans(connectorPlanInput{Extra: extra, SourceDB: "db_bill_1", SourceEngine: SourceEnginePostgres, PGHost: host, PGPort: port, PGUser: "u", PGPassword: "p", SnapshotMode: "no_data"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) == 0 || plans[0].Config["database.hostname"] != "pg.bill.example" {
		t.Fatalf("plans = %#v", plans)
	}
}

func TestApplySourceEndpointOnlyTouchesHostPort(t *testing.T) {
	cfg := map[string]string{
		"database.hostname":  "old.example",
		"database.port":      "5432",
		"database.user":      "repl",
		"slot.name":          "slot_a",
		"publication.name":   "pub_a",
		"table.include.list": "public.t1",
	}
	ApplySourceEndpoint(cfg, "pg.new.example", 5433)
	if cfg["database.hostname"] != "pg.new.example" || cfg["database.port"] != "5433" {
		t.Fatalf("endpoint = %s:%s", cfg["database.hostname"], cfg["database.port"])
	}
	if cfg["slot.name"] != "slot_a" || cfg["publication.name"] != "pub_a" || cfg["database.user"] != "repl" {
		t.Fatalf("unexpected rewrite: %#v", cfg)
	}
	if cfg["table.include.list"] != "public.t1" {
		t.Fatalf("tables changed: %s", cfg["table.include.list"])
	}
}

func TestPickSourceInstance(t *testing.T) {
	bound := &store.Instance{ID: "bound", Host: "10.0.0.1", TenantID: "t1"}
	a := store.Instance{ID: "a", Host: "10.128.33.236", DomainName: "pg.mcamunda.bj.restosuite.cn", Domains: []string{"pg-extra.example"}, TenantID: "t1"}
	b := store.Instance{ID: "b", Host: "10.128.33.237", TenantID: "t1"}
	other := store.Instance{ID: "c", Host: "10.128.33.236", TenantID: "t2"}
	prefix := store.Instance{ID: "d", Host: "10.128.33.2360", TenantID: "t1"}

	if got, reason := PickSourceInstance(bound, []store.Instance{a, b}, "t1", []string{"10.128.33.237"}); got == nil || got.ID != "bound" || reason != "" {
		t.Fatalf("bound id wins, got %#v %q", got, reason)
	}
	if got, reason := PickSourceInstance(nil, []store.Instance{a, b, other, prefix}, "t1", []string{"10.128.33.236"}); got == nil || got.ID != "a" || reason != "" {
		t.Fatalf("host match, got %#v %q", got, reason)
	}
	if got, reason := PickSourceInstance(nil, []store.Instance{a}, "t1", []string{"pg.mcamunda.bj.restosuite.cn"}); got == nil || got.ID != "a" || reason != "" {
		t.Fatalf("primary domain, got %#v %q", got, reason)
	}
	if got, reason := PickSourceInstance(nil, []store.Instance{a}, "t1", []string{"PG-EXTRA.EXAMPLE"}); got == nil || got.ID != "a" || reason != "" {
		t.Fatalf("extra domain, got %#v %q", got, reason)
	}
	if got, reason := PickSourceInstance(nil, []store.Instance{prefix}, "t1", []string{"10.128.33.236"}); got != nil || reason != SourceAddrMissMsg {
		t.Fatalf("prefix must not match, got %#v %q", got, reason)
	}
	if got, reason := PickSourceInstance(nil, []store.Instance{a, other}, "t1", []string{"10.128.33.236"}); got == nil || got.ID != "a" || reason != "" {
		t.Fatalf("other tenant ignored, got %#v %q", got, reason)
	}
	dup := store.Instance{ID: "e", DomainName: "10.128.33.236", TenantID: "t1"}
	if got, reason := PickSourceInstance(nil, []store.Instance{a, dup}, "t1", []string{"10.128.33.236"}); got != nil || reason != SourceAddrMultiMsg {
		t.Fatalf("duplicate addr, got %#v %q", got, reason)
	}
	if got, reason := PickSourceInstance(nil, []store.Instance{a}, "t1", nil); got != nil || reason != SourceUnboundMsg {
		t.Fatalf("no addr, got %#v %q", got, reason)
	}
}

func TestLookupSourceCandidatesDropsSubstring(t *testing.T) {
	list := func(addr string) ([]store.Instance, error) {
		return []store.Instance{
			{ID: "exact", Host: addr},
			{ID: "longer", Host: addr + "9"},
		}, nil
	}
	got, err := LookupSourceCandidates(list, []string{"10.1.1.1", "10.1.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "exact" {
		t.Fatalf("got %#v", got)
	}
}

func TestPubAndSlotUnprobedState(t *testing.T) {
	if pubState(&PipelinePublication{Name: "pub", Unprobed: true, Unreachable: true}) != "UNKNOWN" {
		t.Fatal("unprobed publication")
	}
	if slotState(&PipelineSlot{Name: "slot", Unprobed: true, Message: "未探测"}) != "UNKNOWN" {
		t.Fatal("unprobed slot")
	}
	if slotState(&PipelineSlot{Name: "slot", Active: false}) != "PAUSED" {
		t.Fatal("probed inactive slot")
	}
}
