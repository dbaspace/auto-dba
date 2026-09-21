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
