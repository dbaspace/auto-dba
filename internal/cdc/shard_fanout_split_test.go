package cdc

import "testing"

func TestWantShardFanout(t *testing.T) {
	extra := map[string]string{
		"shard_db":        "true",
		"source_db_names": "shop_000,shop_001",
	}
	if !wantShardFanout("init", "init", extra, true) {
		t.Fatal("expected fanout")
	}
	if wantShardFanout("init", "init", extra, false) {
		t.Fatal("flag off")
	}
	if wantShardFanout("add_table", "init", extra, true) {
		t.Fatal("add_table")
	}
	child := map[string]string{"cdc_fanout_from": "parent", "shard_db": "false"}
	if wantShardFanout("init", "init", child, true) {
		t.Fatal("child must not fanout")
	}
	if wantShardFanout("init", "superseded", extra, true) {
		t.Fatal("superseded")
	}
	if shardFanoutEnabledValue("") != true || shardFanoutEnabledValue("false") != false || shardFanoutEnabledValue("true") != true {
		t.Fatal("flag parse")
	}
}

func TestChildInitExtra(t *testing.T) {
	parent := map[string]string{
		"env":               "test",
		"shard_db":          "true",
		"source_db_names":   "shop_000,shop_001",
		"source_scope_mode": "multi_db",
		"topic_prefix":      "rt_test_shop_000",
		"publication_name":  "custom_pub",
		"slot_name":         "slot_test_shop_000_abc123",
		"connector_name":    "custom_pub",
		"mappings":          `[{"target_table":"tbl_order","source_tables":["tbl_order"],"source_db_scope":"explicit","source_db_names":["shop_001"]}]`,
		"tables":            "tbl_order",
	}
	child := childInitExtra(parent, "sum-1", ShardDbBinding{
		DbName: "shop_001", PublicationName: "rt_test_shop_001", SlotName: "slot_test_shop_001_ffffff", ConnectorName: "rt_test_shop_001",
	}, []string{"tbl_order"})
	if child["cdc_fanout_from"] != "sum-1" || child["topic_prefix"] != "rt_test_shop_000" {
		t.Fatalf("identity=%v", child)
	}
	if child["publication_name"] != "rt_test_shop_001" || child["connector_name"] != "rt_test_shop_001" {
		t.Fatalf("names=%s %s", child["publication_name"], child["connector_name"])
	}
	if child["shard_db"] == "true" || child["source_db_names"] != "" || child["shard_db_bindings"] != "" {
		t.Fatalf("still sharded: %+v", child)
	}
	if child["tables"] != "tbl_order" {
		t.Fatalf("tables=%s", child["tables"])
	}
	maps := ParseMappingsJSON(child["mappings"])
	if len(maps) != 1 || len(maps[0].SourceDbNames) != 0 {
		t.Fatalf("maps=%+v", maps)
	}
}
