package cdc

import (
	"strings"
	"testing"
)

func TestMergeAddTableConnectorConfigShardGroupRegexAndRouter(t *testing.T) {
	cfg := map[string]string{
		"table.include.list":             "public.tbl_heartbeat_table",
		"transforms":                     "unwrap",
		"transforms.unwrap.type":         "io.debezium.transforms.ExtractNewRecordState",
		"transforms.unwrap.add.fields":   "op,source.db,source.table,source.ts_ms",
		"transforms.unwrap.delete.handling.mode": "rewrite",
	}
	maps := []TableMapping{{
		IsShardGroup: true,
		TargetTable:  "tbl_order_tax",
		SourceTables: []string{"tbl_order_tax_0", "tbl_order_tax_1", "tbl_order_tax_63"},
	}}
	tables := []string{"tbl_order_tax_0", "tbl_order_tax_1", "tbl_order_tax_63"}
	if !MergeAddTableConnectorConfig(cfg, "public", tables, maps) {
		t.Fatal("expected config change")
	}

	include := cfg["table.include.list"]
	if !strings.Contains(include, "public.tbl_heartbeat_table") {
		t.Fatalf("missing heartbeat: %s", include)
	}
	wantRegex := "public.tbl_order_tax(_[0-9]+|[0-9]+)"
	if !strings.Contains(include, wantRegex) {
		t.Fatalf("want regex %q in include=%q", wantRegex, include)
	}
	for _, lit := range []string{"public.tbl_order_tax_0", "public.tbl_order_tax_1", "public.tbl_order_tax_63"} {
		if strings.Contains(include, lit) {
			t.Fatalf("literal shard table should not appear: %s in %s", lit, include)
		}
	}

	chain := cfg["transforms"]
	if !strings.Contains(chain, "tbl_order_tax") {
		t.Fatalf("transforms chain missing tbl_order_tax: %q", chain)
	}
	if !strings.HasSuffix(chain, "unwrap") && !strings.Contains(chain, "unwrap") {
		t.Fatalf("transforms should keep unwrap: %q", chain)
	}
	if cfg["transforms.tbl_order_tax.type"] != debeziumTransformByLogicalTableRouter {
		t.Fatalf("type=%q", cfg["transforms.tbl_order_tax.type"])
	}
	if cfg["transforms.tbl_order_tax.topic.regex"] != "(.*)tbl_order_tax(_[0-9]+|[0-9]+)" {
		t.Fatalf("topic.regex=%q", cfg["transforms.tbl_order_tax.topic.regex"])
	}
	if cfg["transforms.tbl_order_tax.topic.replacement"] != "$1tbl_order_tax" {
		t.Fatalf("topic.replacement=%q", cfg["transforms.tbl_order_tax.topic.replacement"])
	}
	if cfg["transforms.tbl_order_tax.key.field.name"] != "source_table" {
		t.Fatalf("key.field.name=%q", cfg["transforms.tbl_order_tax.key.field.name"])
	}

	// idempotent: second merge should not change
	if MergeAddTableConnectorConfig(cfg, "public", tables, maps) {
		t.Fatal("second merge should be no-op")
	}
}

func TestMergeAddTableConnectorConfigPlainTableLiteral(t *testing.T) {
	cfg := map[string]string{"table.include.list": "public.a"}
	changed := MergeAddTableConnectorConfig(cfg, "public", []string{"b"}, []TableMapping{{
		SourceTables: []string{"b"},
		TargetTable:  "b",
	}})
	if !changed {
		t.Fatal("expected change")
	}
	if cfg["table.include.list"] != "public.a,public.b" {
		t.Fatalf("include=%q", cfg["table.include.list"])
	}
	if strings.TrimSpace(cfg["transforms"]) != "" {
		t.Fatalf("plain table should not add transforms: %q", cfg["transforms"])
	}
}
