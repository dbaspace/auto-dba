package cdc

import (
	"strings"
	"testing"
)

func TestResolveShardDBs(t *testing.T) {
	extra := map[string]string{
		"shard_db":          "true",
		"source_db_names":   "db_a_000,db_a_001,db_a_002",
		"representative_db": "db_a_000",
	}
	got := ResolveShardDBs(extra, "db_a_000")
	if len(got) != 3 || got[0] != "db_a_000" {
		t.Fatalf("%v", got)
	}
	if ShardModeActive(map[string]string{"source_scope_mode": "multi_db", "source_db_names": "only"}) {
		t.Fatal("single match must not activate")
	}
}

func TestValidateShardCreate(t *testing.T) {
	if err := ValidateShardCreate(map[string]string{"shard_db": "true", "source_db_names": "a"}, "a"); err == nil {
		t.Fatal("expected error")
	}
	err := ValidateShardCreate(map[string]string{
		"shard_db": "true", "source_db_names": "db_a_0,db_a_1", "source_db_prefix": "db_a_",
	}, "db_a_0")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateShardCreate(map[string]string{
		"shard_db": "true", "source_db_names": "db_order_0,db_ordering_config", "source_db_prefix": "db_order",
	}, "db_order_0"); err == nil {
		t.Fatal("expected reject db_ordering_config")
	}
}

func TestMatchNumericShardName(t *testing.T) {
	cases := []struct {
		name, prefix string
		want         bool
	}{
		{"db_order_0", "db_order_", true},
		{"db_order_63", "db_order_", true},
		{"db_order_0", "db_order", true},
		{"db_ordering_config", "db_order_", false},
		{"db_ordering_config", "db_order", false},
		{"loadtest_db01", "loadtest_db", true},
		{"db_order_", "db_order_", false},
		{"db_order", "db_order", false},
		{"", "db_", false},
		{"db_0", "", false},
	}
	for _, c := range cases {
		if got := MatchNumericShardName(c.name, c.prefix); got != c.want {
			t.Fatalf("MatchNumericShardName(%q,%q)=%v want %v", c.name, c.prefix, got, c.want)
		}
	}
	got := FilterNumericShardNames(
		[]string{"db_order_0", "db_order_1", "db_ordering_config", "other"},
		"db_order_",
	)
	if len(got) != 2 || got[0] != "db_order_0" || got[1] != "db_order_1" {
		t.Fatalf("FilterNumericShardNames: %v", got)
	}
}

func TestShardPrecheckRepOnlyMessage(t *testing.T) {
	// 契约合法时预检只依赖代表库：文案需标明跳过非代表库抽检，避免误以为扫了全部分库。
	extra := map[string]string{
		"shard_db": "true", "source_db_names": "db_a,db_b,db_c",
	}
	if err := ValidateShardCreate(extra, "db_a"); err != nil {
		t.Fatal(err)
	}
	if !ShardModeActive(extra) {
		t.Fatal("expected shard mode")
	}
	dbs := ResolveShardDBs(extra, "db_a")
	if len(dbs) != 3 {
		t.Fatalf("dbs=%v", dbs)
	}
	msg := FormatShardPrecheckRepOnlyMessage(dbs, "db_a")
	if !strings.Contains(msg, "代表库") || !strings.Contains(msg, "跳过") {
		t.Fatalf("msg=%q", msg)
	}
	if !strings.Contains(msg, "3") {
		t.Fatalf("expected db count in msg: %q", msg)
	}
}

func TestValidateMappingShardScopes(t *testing.T) {
	maps := []TableMapping{{
		TargetTable:   "orders",
		SourceDbScope: "explicit",
		SourceDbNames: []string{"db_x"},
	}}
	if err := ValidateMappingShardScopes(maps, []string{"db_a", "db_b"}); err == nil {
		t.Fatal("expected subset error")
	}
	maps[0].SourceDbNames = []string{"db_a"}
	if err := ValidateMappingShardScopes(maps, []string{"db_a", "db_b"}); err != nil {
		t.Fatal(err)
	}
}

func TestTablesForShardDB(t *testing.T) {
	maps := []TableMapping{
		{SourceTables: []string{"t1"}, TargetTable: "t1"},
		{SourceTables: []string{"t2"}, TargetTable: "t2", SourceDbScope: "explicit", SourceDbNames: []string{"db_b"}},
	}
	got := TablesForShardDB(maps, nil, "db_a")
	if len(got) != 1 || got[0] != "t1" {
		t.Fatalf("db_a: %v", got)
	}
	got = TablesForShardDB(maps, nil, "db_b")
	if len(got) != 2 {
		t.Fatalf("db_b: %v", got)
	}
}

func TestBuildShardBindings(t *testing.T) {
	got := BuildShardBindings(map[string]string{
		"shard_db": "true", "source_db_names": "shop_000,shop_001", "env": "test",
	}, "shop_000", "test_shop_000", "slot_test_shop_000_abc123", "test_shop_000")
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].PublicationName != "test_shop_000" || got[1].PublicationName != "test_shop_001" {
		t.Fatalf("%+v", got)
	}
	if got[1].ConnectorName != "test_shop_001" || !strings.HasPrefix(got[1].SlotName, "slot_test_shop_001_") {
		t.Fatalf("other shard=%+v", got[1])
	}
}
