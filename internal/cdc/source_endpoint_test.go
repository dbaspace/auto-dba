package cdc

import "testing"

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
