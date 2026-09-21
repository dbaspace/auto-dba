package handler

import "testing"

func TestHydrateCDCFromMapFlatFields(t *testing.T) {
	t.Parallel()
	raw := map[string]any{
		"task_name":          "demo",
		"task_type":          "add_table",
		"parent_task_id":     "p1",
		"source_instance_id": "src",
		"source_db_name":     "appdb",
		"target_db_name":     "sr",
		"description":        "用户备注",
		"debezium_mode":      "after_only",
		"mappings": []any{
			map[string]any{
				"source_tables":   []any{"orders"},
				"target_table":    "orders",
				"sync_table_mode": "initialization",
			},
		},
	}
	got := hydrateCDCFromMap(raw)
	if got.Name != "demo" || got.TaskType != "add_table" || got.ParentID != "p1" {
		t.Fatalf("%+v", got)
	}
	if got.SourceDB != "appdb" || got.TargetDB != "sr" {
		t.Fatalf("db %+v", got)
	}
	if extra := parseCDCExtraMap(got.Description); extra == nil {
		t.Fatal("extra missing")
	} else if extra["debezium_mode"] != "after_only" {
		t.Fatalf("extra=%v", extra)
	} else if maps := cdcMappingsFromAny(extra["mappings"]); len(maps) != 1 || maps[0].SyncTableMode != "initialization" {
		t.Fatalf("mappings=%+v", extra["mappings"])
	}
}

func TestHydrateCDCFromMapHubTablesAlias(t *testing.T) {
	raw := map[string]any{
		"name":               "t",
		"source_instance_id": "s",
		"tables": []any{
			map[string]any{"source_tables": []any{"a"}, "target_table": "a", "sync_mode": "initial"},
		},
	}
	got := hydrateCDCFromMap(raw)
	extra := parseCDCExtraMap(got.Description)
	maps := cdcMappingsFromAny(extra["mappings"])
	if len(maps) != 1 || maps[0].TargetTable != "a" || maps[0].SyncTableMode != "initialization" {
		t.Fatalf("%+v", maps)
	}
}

func TestNormalizeCDCDescriptionCoercesStringArrays(t *testing.T) {
	t.Parallel()
	desc := `备注
-- auto-dba-cdc:{"tables":"a,b","source_db_names":"db1，db2","mappings":"[{\"target_table\":\"t1\"}]"}`
	got := normalizeCDCDescription(desc)
	extra := parseCDCExtraMap(got)
	if extra == nil {
		t.Fatal("extra missing")
	}
	if tables, ok := extra["tables"].([]any); !ok || len(tables) != 2 || tables[0] != "a" || tables[1] != "b" {
		t.Fatalf("tables=%#v", extra["tables"])
	}
	if names, ok := extra["source_db_names"].([]any); !ok || len(names) != 2 || names[0] != "db1" || names[1] != "db2" {
		t.Fatalf("source_db_names=%#v", extra["source_db_names"])
	}
	maps := extra["mappings"]
	arr, ok := maps.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("mappings=%#v", maps)
	}
	m, ok := arr[0].(map[string]any)
	if !ok || m["target_table"] != "t1" {
		t.Fatalf("mapping item=%#v", arr[0])
	}
	if normalizeCDCDescription(got) != got {
		t.Fatal("second normalize should be idempotent")
	}
}

func TestNormalizeCDCDescriptionKeepsPlainText(t *testing.T) {
	t.Parallel()
	if got := normalizeCDCDescription("plain"); got != "plain" {
		t.Fatalf("got %q", got)
	}
}
