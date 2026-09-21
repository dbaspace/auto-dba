package cdc

import (
	"regexp"
	"strings"
	"testing"
)

func TestShardTableRegexPart(t *testing.T) {
	got := shardTableRegexPart("tbl_orders")
	want := "tbl_orders(_[0-9]+|[0-9]+)"
	if got != want {
		t.Fatalf("shardTableRegexPart: got %q want %q", got, want)
	}
}

func TestDeriveShardRegexPart_default(t *testing.T) {
	got := deriveShardRegexPart("tbl_orders", "", nil)
	want := "tbl_orders(_[0-9]+|[0-9]+)"
	if got != want {
		t.Fatalf("deriveShardRegexPart default: got %q want %q", got, want)
	}
}

func TestDeriveShardRegexPart_fromSourceTables(t *testing.T) {
	got := deriveShardRegexPart("ods_tbl_order", "", []string{"tbl_order_0", "tbl_order_1"})
	want := "tbl_order(_[0-9]+|[0-9]+)"
	if got != want {
		t.Fatalf("deriveShardRegexPart from source: got %q want %q", got, want)
	}
}

func TestInferSourceRegexStem(t *testing.T) {
	if got := inferSourceRegexStem([]string{"tbl_order_0", "tbl_order_1"}); got != "tbl_order" {
		t.Fatalf("multi source stem: %q", got)
	}
	if got := inferSourceTableStem("tbl_test0"); got != "tbl_test" {
		t.Fatalf("tbl_test0 stem: %q", got)
	}
}

func TestDebeziumShardRegexFromPattern(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"tbl_points_account*", "tbl_points_account(_[0-9]+|[0-9]+)"},
		{"tbl_points_account_*", "tbl_points_account(_[0-9]+|[0-9]+)"},
		{"tbl_custom_[0-9]+", "tbl_custom_[0-9]+"},
		{"tbl_orders(_[0-9]+|[0-9]+)", "tbl_orders(_[0-9]+|[0-9]+)"},
		{"tbl_*_log", "tbl_.*_log"},
		{"tbl_order", "tbl_order(_[0-9]+|[0-9]+)"},
	}
	for _, c := range cases {
		got := debeziumShardRegexFromPattern(c.in)
		if got != c.want {
			t.Errorf("debeziumShardRegexFromPattern(%q)=%q want %q", c.in, got, c.want)
		}
		if got != "" {
			if _, err := regexp.Compile(got); err != nil {
				t.Errorf("result %q not a valid regex: %v", got, err)
			}
		}
	}
}

func TestFinalizeDebeziumTransformsChain(t *testing.T) {
	cases := []struct {
		prefix  string
		routers []string
		unwrap  bool
		want    string
	}{
		{"", nil, true, "unwrap"},
		{"", []string{"tbl_orders"}, true, "tbl_orders,unwrap"},
		{"filter", nil, true, "filter,unwrap"},
		{"filter", []string{"tbl_orders"}, true, "filter,tbl_orders,unwrap"},
		{"", []string{"tbl_orders"}, false, "tbl_orders"},
		{"filter,unwrap", []string{"tbl_orders"}, true, "filter,tbl_orders,unwrap"},
	}
	for _, tc := range cases {
		got := finalizeDebeziumTransformsChain(tc.prefix, tc.routers, tc.unwrap)
		if got != tc.want {
			t.Fatalf("finalize(%q,%v,%v)=%q want %q", tc.prefix, tc.routers, tc.unwrap, got, tc.want)
		}
	}
}

func TestBuildDebeziumConfigAfterOnly_matchesOpsTemplate(t *testing.T) {
	cfg := BuildConfigAfterOnly(DebeziumBuildParams{
		PGHost:                 "10.0.0.1",
		PGPort:                 5432,
		PGUser:                 "repl",
		PGPassword:             "secret",
		PGDbName:               "order_db",
		Schema:                 "public",
		Tables:                 []string{"tbl_orders"},
		PublicationName:        "prod_order_db",
		SlotName:               "slot_prod_order_db",
		TopicPrefix:            "prod_order_db",
		SnapshotMode:           "",
		TopicPartitions:        6,
		TopicReplicationFactor: 2,
	})
	if cfg["connector.class"] != "io.debezium.connector.postgresql.PostgresConnector" {
		t.Fatalf("connector.class: %q", cfg["connector.class"])
	}
	if cfg["database.hostname"] != "10.0.0.1" || cfg["database.dbname"] != "order_db" {
		t.Fatalf("db connection fields: %+v", cfg)
	}
	if cfg["table.include.list"] != "public.tbl_orders" {
		t.Fatalf("table.include.list: %q", cfg["table.include.list"])
	}
	if cfg["snapshot.mode"] != SnapshotNever {
		t.Fatalf("snapshot.mode: %q", cfg["snapshot.mode"])
	}
	if cfg["publication.autocreate.mode"] != "disabled" {
		t.Fatalf("publication.autocreate.mode: %q", cfg["publication.autocreate.mode"])
	}
	if cfg["transforms"] != "unwrap" {
		t.Fatalf("transforms: %q", cfg["transforms"])
	}
	if cfg["transforms.unwrap.add.fields"] != "op,source.db,source.table,source.ts_ms" {
		t.Fatalf("add.fields: %q", cfg["transforms.unwrap.add.fields"])
	}
	if _, ok := cfg["transforms.filter.type"]; ok {
		t.Fatal("after_only template should not include filter")
	}
	if _, ok := cfg["heartbeat.interval.ms"]; ok {
		t.Fatal("after_only template should not include heartbeat.interval.ms")
	}
}

func TestBuildDebeziumConfigAfterOnly_shardRouter(t *testing.T) {
	cfg := BuildConfigAfterOnly(DebeziumBuildParams{
		PGHost:          "h",
		PGPort:          5432,
		PGUser:          "u",
		PGPassword:      "p",
		PGDbName:        "db",
		Schema:          "public",
		Tables:          []string{"shift_reminder_deliveries"},
		PublicationName: "pub",
		SlotName:        "slot",
		TopicPrefix:     "test_db_dba_000",
		ShardGroups: []ShardGroup{
			{LogicalName: "tbl_orders", Regex: deriveShardRegexPart("tbl_orders", "", []string{"tbl_orders0", "tbl_orders_001"})},
		},
	})
	wantRegex := "tbl_orders(_[0-9]+|[0-9]+)"
	if cfg["table.include.list"] != "public.shift_reminder_deliveries,public."+wantRegex {
		t.Fatalf("table.include.list: %q", cfg["table.include.list"])
	}
	if cfg["transforms"] != "tbl_orders,unwrap" {
		t.Fatalf("transforms: %q", cfg["transforms"])
	}
	if cfg["transforms.tbl_orders.topic.regex"] != "(.*)"+wantRegex {
		t.Fatalf("topic.regex: %q", cfg["transforms.tbl_orders.topic.regex"])
	}
	if cfg["transforms.tbl_orders.topic.replacement"] != "$1tbl_orders" {
		t.Fatalf("topic.replacement: %q", cfg["transforms.tbl_orders.topic.replacement"])
	}
}

func TestSplitMappings_derivesShardRegex(t *testing.T) {
	plain, shards := SplitMappings([]TableMapping{{
		SourceTables: []string{"tbl_orders0", "tbl_orders_001"},
		TargetTable:  "tbl_orders",
		IsShardGroup: true,
	}}, nil)
	if len(plain) != 0 {
		t.Fatalf("plain tables: %v", plain)
	}
	if len(shards) != 1 || shards[0].LogicalName != "tbl_orders" {
		t.Fatalf("shards: %+v", shards)
	}
	if shards[0].Regex != "tbl_orders(_[0-9]+|[0-9]+)" {
		t.Fatalf("shard regex: %q", shards[0].Regex)
	}
}

func TestBuildPreview_masksPassword(t *testing.T) {
	out := BuildPreview(PreviewInput{
		Name:         "prod_order",
		Env:          "prod",
		SourceDB:     "order_db",
		SourceSchema: "public",
		PGHost:       "10.0.0.1",
		PGPort:       5432,
		PGUser:       "repl",
		DebeziumMode: ModeAfterOnly,
		Tables:       []string{"tbl_orders"},
	})
	if out.Template != "debezium_after_only" {
		t.Fatalf("template: %q", out.Template)
	}
	if out.Config["database.password"] != MaskedPassword {
		t.Fatalf("password should be masked: %q", out.Config["database.password"])
	}
	if out.PublicationName != "prod_order_db" || out.TopicPrefix != "prod_order_db" {
		t.Fatalf("names: pub=%q slot=%q topic=%q", out.PublicationName, out.SlotName, out.TopicPrefix)
	}
	if !strings.HasPrefix(out.SlotName, "slot_prod_order_db_") {
		t.Fatalf("slot=%q", out.SlotName)
	}
}
