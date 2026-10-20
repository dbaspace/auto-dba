package cdc

import "testing"

func TestFilterTablesByPatternGlob(t *testing.T) {
	all := []string{"tv_app_market_0", "tv_app_market_1", "tv_app_market_extra", "other"}
	got, err := FilterTablesByPattern(all, "tv_app_market_*", "glob")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "tv_app_market_0" || got[1] != "tv_app_market_1" {
		t.Fatalf("narrow glob: %v", got)
	}
}

func TestFilterTablesByPatternRegex(t *testing.T) {
	all := []string{"orders_a", "orders_b", "users"}
	got, err := FilterTablesByPattern(all, `^orders_`, "regex")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestCollectTaskKafkaTopics(t *testing.T) {
	topics := CollectTaskKafkaTopics("cdc", "public", []TableMapping{
		{SourceTables: []string{"orders"}, TargetTable: "orders"},
		{SourceTables: []string{"t0", "t1"}, TargetTable: "t", IsShardGroup: true},
	}, nil)
	if len(topics) != 2 {
		t.Fatalf("%v", topics)
	}
}

func TestCollectMappingKafkaTopics_sourceNeTargetUsesLogical(t *testing.T) {
	// 对齐 hub：tbl_x0 → tbl_x 未标 is_shard_group 也只期望 logical topic
	got := CollectMappingKafkaTopics("rt_test_db_order_master", "public", TableMapping{
		SourceTables: []string{"tbl_order_0"},
		TargetTable:  "tbl_order",
	})
	want := "rt_test_db_order_master.public.tbl_order"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v want [%s]", got, want)
	}
}

func TestCollectMappingKafkaTopics_oneToOneSameName(t *testing.T) {
	got := CollectMappingKafkaTopics("cdc", "public", TableMapping{
		SourceTables: []string{"orders"},
		TargetTable:  "orders",
	})
	if len(got) != 1 || got[0] != "cdc.public.orders" {
		t.Fatalf("%v", got)
	}
}

func TestIsShardMapping(t *testing.T) {
	if !IsShardMapping(TableMapping{SourceTables: []string{"a0"}, TargetTable: "a"}) {
		t.Fatal("source≠target should be shard")
	}
	if IsShardMapping(TableMapping{SourceTables: []string{"a"}, TargetTable: "a"}) {
		t.Fatal("1:1 same name should not be shard")
	}
	if !IsShardMapping(TableMapping{SourceTables: []string{"a"}, TargetTable: "a", ShardPattern: "a_*"}) {
		t.Fatal("pattern should be shard")
	}
}

func TestPipelineActiveMappings_skipsDeletedAndFailed(t *testing.T) {
	maps := []TableMapping{
		{SourceTables: []string{"ok"}, TargetTable: "ok"},
		{SourceTables: []string{"gone"}, TargetTable: "gone", Status: "deleted"},
		{SourceTables: []string{"bad"}, TargetTable: "bad", Status: "failed"},
	}
	active := PipelineActiveMappings(maps)
	if len(active) != 1 || active[0].TargetTable != "ok" {
		t.Fatalf("%v", active)
	}
	topics := CollectPipelineTopicNames("cdc", "public", active, nil)
	if len(topics) != 1 || topics[0] != "cdc.public.ok" {
		t.Fatalf("%v", topics)
	}
}

func TestCollectPipelineTopicNames_fallbackStored(t *testing.T) {
	// mappings 无 source/target 可算时，回退落库 kafka_topics
	maps := []TableMapping{
		{KafkaTopics: []string{"rt.public.stored_a", "rt.public.stored_b"}},
	}
	got := CollectPipelineTopicNames("rt", "public", maps, nil)
	if len(got) != 2 || got[0] != "rt.public.stored_a" || got[1] != "rt.public.stored_b" {
		t.Fatalf("%v", got)
	}
}

func TestIsProtectedPublicationTable(t *testing.T) {
	if !IsProtectedPublicationTable("debezium_signal") || !IsProtectedPublicationTable("public.debezium_signal") {
		t.Fatal("signal should be protected")
	}
	if IsProtectedPublicationTable("orders") {
		t.Fatal("orders should not be protected")
	}
}

func TestSummarizeSchemaAlertsState(t *testing.T) {
	state, _, n := SummarizeSchemaAlertsState(nil, nil)
	if state != SchemaAlertOK || n != 0 {
		t.Fatalf("%s %d", state, n)
	}
	state, _, n = SummarizeSchemaAlertsState([]SchemaAlertItem{{HasDrift: true, MissingCount: 1}}, nil)
	if state != SchemaAlertWarn || n != 1 {
		t.Fatalf("%s %d", state, n)
	}
}
