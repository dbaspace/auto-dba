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
