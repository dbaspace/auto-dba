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
