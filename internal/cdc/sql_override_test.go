package cdc

import "testing"

func TestParseAndApplySQLOverrides(t *testing.T) {
	raw := `{"orders":{"create_table_sql":"CREATE TABLE custom;","routine_load_sql":"CREATE ROUTINE LOAD x ON t PROPERTIES (\"kafka_topic\" = \"p.public.orders\") FROM KAFKA();"}}`
	ovs := ParseSQLOverrides(raw)
	if len(ovs) != 1 || ovs[0].TargetTable != "orders" {
		t.Fatalf("overrides=%+v", ovs)
	}
	items := []TargetSQLItem{{
		Table: "orders",
		DDL:   "CREATE TABLE auto;",
		Load:  `CREATE ROUTINE LOAD x ON t PROPERTIES ("kafka_topic" = "p.public.orders") FROM KAFKA();`,
	}}
	got := ApplySQLOverrides(items, ovs)
	if !got[0].DDLCustomized || got[0].DDL != "CREATE TABLE custom;" {
		t.Fatalf("ddl not overridden: %+v", got[0])
	}
	if !got[0].LoadCustomized {
		t.Fatalf("load not overridden: %+v", got[0])
	}
}

func TestApplySQLOverridesRejectsStaleTopic(t *testing.T) {
	items := []TargetSQLItem{{
		Table: "orders",
		Load:  `CREATE ROUTINE LOAD x ON t PROPERTIES ("kafka_topic" = "new.public.orders") FROM KAFKA();`,
	}}
	got := ApplySQLOverrides(items, []SQLOverride{{
		TargetTable:    "orders",
		RoutineLoadSQL: `CREATE ROUTINE LOAD x ON t PROPERTIES ("kafka_topic" = "old.public.orders") FROM KAFKA();`,
	}})
	if got[0].LoadCustomized {
		t.Fatal("stale kafka_topic should not override")
	}
	if got[0].Reason == "" {
		t.Fatal("expected warning reason")
	}
}
