package cdc

import "testing"

func TestBuildDebeziumConfigBeforeAfter_plain(t *testing.T) {
	cfg := BuildConfigBeforeAfter(DebeziumBuildParams{
		PGHost:                 "pg.example.com",
		PGPort:                 5432,
		PGUser:                 "u",
		PGPassword:             "p",
		PGDbName:               "db1",
		Schema:                 "public",
		Tables:                 []string{"tbl_a"},
		PublicationName:        "pub1",
		SlotName:               "slot1",
		TopicPrefix:            "pfx",
		TopicPartitions:        6,
		TopicReplicationFactor: 2,
	})
	if cfg["snapshot.mode"] != SnapshotNever {
		t.Fatalf("snapshot.mode: %q", cfg["snapshot.mode"])
	}
	if cfg["signal.data.collection"] != "public.debezium_signal" {
		t.Fatalf("signal: %q", cfg["signal.data.collection"])
	}
	if _, ok := cfg["transforms"]; ok {
		t.Fatalf("plain before_after without filter should not include transforms, got %q", cfg["transforms"])
	}
	for _, k := range []string{
		"transforms.filter.type",
		"transforms.filter.language",
		"transforms.filter.condition",
		"transforms.unwrap.type",
		"heartbeat.interval.ms",
		"key.converter",
		"value.converter",
	} {
		if _, ok := cfg[k]; ok {
			t.Fatalf("before_after should not include %s", k)
		}
	}
}

func TestBuildDebeziumConfigBeforeAfter_withFilter(t *testing.T) {
	cfg := BuildConfigBeforeAfter(DebeziumBuildParams{
		PGHost: "h", PGPort: 5432, PGUser: "u", PGPassword: "p", PGDbName: "db",
		Tables: []string{"t"}, PublicationName: "pub", SlotName: "slot", TopicPrefix: "pfx",
		FilterDeleteEvents: true,
	})
	if cfg["transforms"] != "filter" {
		t.Fatalf("transforms: %q", cfg["transforms"])
	}
	if cfg["transforms.filter.condition"] != debeziumFilterSMTCondition {
		t.Fatalf("filter condition: %q", cfg["transforms.filter.condition"])
	}
}

func TestBuildDebeziumConfigBeforeAfter_shardRouter(t *testing.T) {
	target := "tbl_orders"
	cfg := BuildConfigBeforeAfter(DebeziumBuildParams{
		PGHost:          "h",
		PGPort:          5432,
		PGUser:          "u",
		PGPassword:      "p",
		PGDbName:        "db",
		Schema:          "public",
		Tables:          []string{"other_tbl"},
		PublicationName: "pub",
		SlotName:        "slot",
		TopicPrefix:     "pfx",
		ShardGroups: []ShardGroup{
			{LogicalName: target, Regex: deriveShardRegexPart(target, "", []string{"tbl_orders0", "tbl_orders_001"})},
		},
	})
	if cfg["transforms"] != target {
		t.Fatalf("transforms: %q", cfg["transforms"])
	}
	if cfg["transforms."+target+".type"] != debeziumTransformByLogicalTableRouter {
		t.Fatalf("router type: %q", cfg["transforms."+target+".type"])
	}
}

func TestBuildDebeziumConfigBeforeAfter_shardRouterWithFilter(t *testing.T) {
	target := "tbl_orders"
	cfg := BuildConfigBeforeAfter(DebeziumBuildParams{
		PGHost: "h", PGPort: 5432, PGUser: "u", PGPassword: "p", PGDbName: "db",
		Schema: "public", Tables: []string{"other_tbl"},
		PublicationName: "pub", SlotName: "slot", TopicPrefix: "pfx",
		FilterDeleteEvents: true,
		ShardGroups: []ShardGroup{
			{LogicalName: target, Regex: deriveShardRegexPart(target, "", []string{"tbl_orders0"})},
		},
	})
	if cfg["transforms"] != "filter,"+target {
		t.Fatalf("transforms: %q", cfg["transforms"])
	}
}

func TestBuildDebeziumConfigBeforeAfter_matchesOpsTemplate(t *testing.T) {
	cfg := BuildConfigBeforeAfter(DebeziumBuildParams{
		PGHost:                 "pg.example.com",
		PGPort:                 5432,
		PGUser:                 "repl",
		PGPassword:             "secret",
		PGDbName:               "mydb",
		Schema:                 "public",
		Tables:                 []string{"tbl_orders"},
		PublicationName:        "pub_cdc",
		SlotName:               "slot_cdc",
		TopicPrefix:            "test_mydb",
		TopicPartitions:        6,
		TopicReplicationFactor: 2,
	})
	wantKeys := []string{
		"connector.class",
		"database.hostname",
		"database.port",
		"database.user",
		"database.password",
		"database.dbname",
		"table.include.list",
		"topic.prefix",
		"snapshot.mode",
		"topic.creation.default.partitions",
		"topic.creation.default.replication.factor",
		"signal.data.collection",
		"plugin.name",
		"publication.name",
		"publication.autocreate.mode",
		"slot.name",
		"time.precision.mode",
		"tombstones.on.delete",
		"decimal.handling.mode",
	}
	for _, k := range wantKeys {
		if _, ok := cfg[k]; !ok {
			t.Fatalf("ops template missing key %q", k)
		}
	}
	for _, forbidden := range []string{
		"transforms",
		"transforms.filter.type",
		"transforms.unwrap.type",
		"heartbeat.interval.ms",
		"key.converter",
		"value.converter",
	} {
		if _, ok := cfg[forbidden]; ok {
			t.Fatalf("default before_after template must not include %s", forbidden)
		}
	}
}

func TestBuildConfig_dispatchesBeforeAfter(t *testing.T) {
	cfg := BuildConfig(ModeBeforeAfter, DebeziumBuildParams{
		PGHost: "h", PGPort: 5432, PGUser: "u", PGPassword: "p", PGDbName: "db",
		Tables: []string{"t"}, PublicationName: "pub", SlotName: "slot", TopicPrefix: "pfx",
	})
	if _, ok := cfg["transforms"]; ok {
		t.Fatalf("default before_after should not set transforms, got %q", cfg["transforms"])
	}
}

func TestApplyBeforeAfterDeleteFilter_autoRouterChain(t *testing.T) {
	target := "tbl_pay_order_refund"
	cfg := map[string]string{
		"transforms." + target + ".type":              debeziumTransformByLogicalTableRouter,
		"transforms." + target + ".topic.replacement": "$1" + target,
		"transforms." + target + ".key.field.name":    "source_table",
		"transforms." + target + ".topic.regex":       "(.*)" + target + "_[0-9]*",
	}
	applyBeforeAfterDeleteFilter(cfg, false)
	if err := validateAndNormalizeConnectorCfg(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["transforms"] != target {
		t.Fatalf("transforms: got %q want %q", cfg["transforms"], target)
	}
	for _, k := range []string{"transforms.filter.type", "transforms.filter.language", "transforms.filter.condition"} {
		if _, ok := cfg[k]; ok {
			t.Fatalf("should not include %s", k)
		}
	}
}

func TestResolveFilterDeleteEvents(t *testing.T) {
	if ResolveFilterDeleteEvents(ModeAfterOnly, true) {
		t.Fatal("after_only should not enable filter")
	}
	if !ResolveFilterDeleteEvents(ModeBeforeAfter, true) {
		t.Fatal("before_after should allow filter when requested")
	}
	if ResolveFilterDeleteEvents(ModeBeforeAfter, false) {
		t.Fatal("before_after should respect false")
	}
}
