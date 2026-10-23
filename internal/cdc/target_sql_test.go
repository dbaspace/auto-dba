package cdc

import (
	"strings"
	"testing"
)

func TestBuildTargetSQLItems_usesMappingKeysAndPartition(t *testing.T) {
	items := BuildTargetSQLItems(TargetSQLParams{
		TargetDB:           "cdc_db",
		KeyModel:           "unique_key",
		Buckets:            10,
		ReplicationNum:     1,
		RoutineLoadConc:    3,
		KafkaBrokers:       "k1:9092",
		TopicPrefix:        "test_db",
		Env:                "test",
		SourceDB:           "db",
		SourceSchema:       "public",
		DebeziumMode:       "after_only",
		MessageSchemaMode:  "with_schema",
		AutoCreate:         true,
		DynamicPartitionOn: true,
		TaskPartitionCol:   "created_at",
		Mappings: []TableMapping{{
			SourceTables: []string{"orders"},
			TargetTable:  "orders",
			KeyColumns:   []string{"id", "tenant_id"},
			ShardKeys:    []string{"tenant_id"},
		}},
	}, map[string][]pgCol{
		"orders": {{Name: "id", UDT: "int8"}, {Name: "tenant_id", UDT: "int8"}, {Name: "created_at", UDT: "timestamp"}},
	}, map[string][]string{"orders": {"id"}})
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	ddl := items[0].DDL
	if !strings.Contains(ddl, "UNIQUE KEY(`id`,`tenant_id`)") {
		t.Fatalf("key columns not used: %s", ddl)
	}
	if !strings.Contains(ddl, "DISTRIBUTED BY HASH(`tenant_id`)") {
		t.Fatalf("shard keys not used: %s", ddl)
	}
	if !strings.Contains(ddl, "PARTITION BY RANGE(`created_at`)") {
		t.Fatalf("partition missing: %s", ddl)
	}
	if !strings.Contains(ddl, `"dynamic_partition.enable" = "true"`) {
		t.Fatalf("dp props missing: %s", ddl)
	}
	if !strings.Contains(items[0].Load, `"kafka_broker_list" = "k1:9092"`) {
		t.Fatalf("kafka missing: %s", items[0].Load)
	}
	if !strings.Contains(items[0].Load, "test_db.public.orders") {
		t.Fatalf("topic missing: %s", items[0].Load)
	}
	if !strings.Contains(items[0].Load, "jsonpaths") {
		t.Fatalf("with_schema should have jsonpaths: %s", items[0].Load)
	}
	if !strings.Contains(items[0].Load, "`rl_test_db_orders`") {
		t.Fatalf("routine load name should be rl_test_db_orders: %s", items[0].Load)
	}
	if strings.Contains(items[0].Load, "orders_rl") {
		t.Fatalf("routine load must not use _rl suffix: %s", items[0].Load)
	}
}

func TestBuildTargetSQLItems_withoutSchemaOmitsJSONPaths(t *testing.T) {
	items := BuildTargetSQLItems(TargetSQLParams{
		TargetDB: "cdc_db", KeyModel: "unique_key", KafkaBrokers: "k:9092",
		TopicPrefix: "t", SourceSchema: "public", DebeziumMode: "after_only",
		MessageSchemaMode: "without_schema", AutoCreate: true,
		Mappings: []TableMapping{{SourceTables: []string{"orders"}, TargetTable: "orders", KeyColumns: []string{"id"}}},
	}, map[string][]pgCol{"orders": {{Name: "id", UDT: "int8"}}}, map[string][]string{"orders": {"id"}})
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	if strings.Contains(items[0].Load, "jsonpaths") {
		t.Fatalf("without_schema after_only must not have jsonpaths: %s", items[0].Load)
	}
}

func TestBuildTargetSQLItems_addTableShortNameAndSkipsSignal(t *testing.T) {
	items := BuildTargetSQLItems(TargetSQLParams{
		TaskType: "add_table", TargetDB: "db_ods", KeyModel: "unique_key",
		KafkaBrokers: "k:9092", TopicPrefix: "rt_bj_db_member_profile",
		Env: "beijing", SourceDB: "db_member_profile", SourceSchema: "public",
		AutoCreate: true,
		Mappings: []TableMapping{
			{SourceTables: []string{"tbl_authorization"}, TargetTable: "tbl_authorization", KeyColumns: []string{"id"}},
			{SourceTables: []string{"debezium_signal"}, TargetTable: "debezium_signal", KeyColumns: []string{"id"}},
		},
	}, map[string][]pgCol{
		"tbl_authorization": {{Name: "id", UDT: "int8"}},
		"debezium_signal":   {{Name: "id", UDT: "varchar"}},
	}, nil)
	if len(items) != 1 {
		t.Fatalf("signal table should be omitted, items=%d", len(items))
	}
	if !strings.Contains(items[0].Load, "`rl_tbl_authorization`") {
		t.Fatalf("add table rl name: %s", items[0].Load)
	}
	if strings.Contains(items[0].Load, "rt_bj_db_member_profile_tbl") {
		t.Fatalf("add table must not use topic prefix in rl name: %s", items[0].Load)
	}
}

func TestBuildTargetSQLItems_varcharCommentShardAndKeyOrder(t *testing.T) {
	items := BuildTargetSQLItems(TargetSQLParams{
		TargetDB: "ods", KeyModel: "primary_key", KafkaBrokers: "k:9092",
		TopicPrefix: "pfx", SourceSchema: "public", AutoCreate: true,
		Mappings: []TableMapping{{
			SourceTables: []string{"tbl_order_000", "tbl_order_001"},
			TargetTable:  "tbl_order",
			KeyColumns:   []string{"id"},
		}},
	}, map[string][]pgCol{
		"tbl_order_000": {
			{Name: "note", DataType: "text", UDT: "text", IsNullable: true, Comment: "it's ok"},
			{Name: "id", DataType: "bigint", UDT: "int8", IsNullable: false, IsIdentity: true, Comment: "主键"},
			{Name: "title", DataType: "character varying", UDT: "varchar", CharMaxLen: 20, IsNullable: false, Comment: "标题"},
			{Name: "only_000", DataType: "integer", UDT: "int4"},
			{Name: "amount", DataType: "numeric", UDT: "numeric", NumPrecision: 10, NumScale: 2, IsNullable: true},
			{Name: "flag", DataType: "smallint", UDT: "int2", IsNullable: false, DefaultExpr: "0"},
			{Name: "ratio", DataType: "real", UDT: "float4", IsNullable: true},
			{Name: "report_date", DataType: "date", UDT: "date", IsNullable: false},
			{Name: "payload", DataType: "jsonb", UDT: "jsonb", IsNullable: false, DefaultExpr: "'{}'::jsonb"},
		},
		"tbl_order_001": {
			{Name: "note", DataType: "text", UDT: "text", IsNullable: true},
			{Name: "id", DataType: "bigint", UDT: "int8", IsNullable: false},
			{Name: "title", DataType: "character varying", UDT: "varchar", CharMaxLen: 20, IsNullable: false},
			{Name: "amount", DataType: "numeric", UDT: "numeric", NumPrecision: 10, NumScale: 2, IsNullable: true},
			{Name: "flag", DataType: "smallint", UDT: "int2"},
			{Name: "ratio", DataType: "real", UDT: "float4", IsNullable: true},
			{Name: "report_date", DataType: "date", UDT: "date", IsNullable: false},
			{Name: "payload", DataType: "jsonb", UDT: "jsonb", IsNullable: false},
		},
	}, map[string][]string{"tbl_order_000": {"id"}})
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	if items[0].Table != "tbl_order" {
		t.Fatalf("target=%s", items[0].Table)
	}
	ddl := items[0].DDL
	if strings.Contains(ddl, "tbl_order_000") || strings.Contains(ddl, "only_000") {
		t.Fatalf("shard-only name or column leaked:\n%s", ddl)
	}
	idAt := strings.Index(ddl, "`id`")
	titleAt := strings.Index(ddl, "`title`")
	if idAt < 0 || titleAt < 0 || idAt > titleAt {
		t.Fatalf("key column should lead: %s", ddl)
	}
	for _, want := range []string{
		"`title` VARCHAR(60) NOT NULL COMMENT '标题'",
		"`id` BIGINT NOT NULL COMMENT '主键'",
		"`note` STRING NULL COMMENT 'it''s ok'",
		"`amount` DECIMAL(10,2) NULL",
		"`flag` SMALLINT NOT NULL DEFAULT \"0\"",
		"`ratio` FLOAT NULL",
		"`report_date` DATE NOT NULL DEFAULT \"1970-01-01\"",
		"`payload` JSON NOT NULL",
		"PRIMARY KEY(`id`)",
	} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("missing %q\n%s", want, ddl)
		}
	}
	if strings.Contains(ddl, "`id` BIGINT NOT NULL DEFAULT") {
		t.Fatalf("identity should not have default: %s", ddl)
	}
	if strings.Contains(ddl, "`payload` JSON NOT NULL DEFAULT") {
		t.Fatalf("json should not have default: %s", ddl)
	}
}

func TestResolveKafkaBootstrap(t *testing.T) {
	if got := ResolveKafkaBootstrap("a:9092", "b:9092", "c:9092"); got != "a:9092" {
		t.Fatal(got)
	}
	if got := ResolveKafkaBootstrap("", "b:9092", "c:9092"); got != "b:9092" {
		t.Fatal(got)
	}
	if got := ResolveKafkaBootstrap("", "", "c:9092"); got != "c:9092" {
		t.Fatal(got)
	}
	if got := ResolveKafkaBootstrap("", "10.100.112.17:9092:9092", ""); got != "10.100.112.17:9092" {
		t.Fatalf("dup port: %s", got)
	}
}
