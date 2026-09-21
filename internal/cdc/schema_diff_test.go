package cdc

import "testing"

func TestDiffPGMinusSR(t *testing.T) {
	yes := true
	pg := []ColumnInfo{
		{Name: "id", DataType: "int8", IsPrimaryKey: true},
		{Name: "name", DataType: "text", IsNullable: &yes},
		{Name: "created_at", DataType: "timestamp"},
	}
	got := DiffPGMinusSR(pg, []string{"name"})
	if len(got) != 1 || got[0].Name != "created_at" || got[0].SRType != "DATETIME" {
		t.Fatalf("%+v", got)
	}
}

func TestBuildAlterAddColumnSQL(t *testing.T) {
	sql := BuildAlterAddColumnSQL("db", "t", []SchemaDiffColumn{{Name: "c1", SRType: "STRING"}})
	if sql != "ALTER TABLE `db`.`t` ADD COLUMN `c1` STRING;" {
		t.Fatal(sql)
	}
}

func TestMapPGTypeToSR(t *testing.T) {
	if MapPGTypeToSR("int8") != "BIGINT" {
		t.Fatal(MapPGTypeToSR("int8"))
	}
}

func TestBuildAlterMigratePlan(t *testing.T) {
	plan := BuildAlterMigratePlan("db", "t", "k:9092", "pref.public.t", "after_only", []string{"id"}, []SchemaDiffColumn{{Name: "c1", SRType: "STRING", Selected: true}}, 3)
	if plan["alter_sql"] == "" || plan["stop_sql"] == "" || plan["create_sql"] == "" {
		t.Fatalf("%+v", plan)
	}
}
