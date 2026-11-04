package cdc

import (
	"encoding/json"
	"testing"
)

func TestSnapshotMappingsForAddTable(t *testing.T) {
	mapsJSON := `[{"target_table":"t1","source_tables":["t1"],"sync_table_mode":"incremental"},{"target_table":"t2","source_tables":["t2"],"sync_table_mode":"initialization"}]`
	got := SnapshotMappingsForAddTable("add_table", map[string]string{"mappings": mapsJSON, "snapshot_mode": "never"})
	if len(got) != 1 || got[0].TargetTable != "t2" {
		t.Fatalf("init mapping wins: %#v", got)
	}
	allInc := `[{"target_table":"t1","source_tables":["t1"],"sync_table_mode":"incremental"}]`
	if got := SnapshotMappingsForAddTable("add_table", map[string]string{"mappings": allInc, "snapshot_mode": "never"}); len(got) != 0 {
		t.Fatalf("never should skip: %#v", got)
	}
	if got := SnapshotMappingsForAddTable("add_table", map[string]string{"mappings": allInc, "snapshot_mode": "initial"}); len(got) != 1 {
		t.Fatalf("initial covers legacy incremental maps: %#v", got)
	}
	if got := SnapshotMappingsForAddTable("add_table", map[string]string{"mappings": allInc}); len(got) != 1 {
		t.Fatalf("empty snapshot_mode covers legacy: %#v", got)
	}
	if got := SnapshotMappingsForAddTable("init", map[string]string{"mappings": allInc, "snapshot_mode": "initial"}); len(got) != 0 {
		t.Fatalf("non-add_table without init maps: %#v", got)
	}
}

func TestNormalizeSyncTableMode(t *testing.T) {
	cases := map[string]string{
		"":                 SyncTableModeIncremental,
		"incremental":      SyncTableModeIncremental,
		"INCREMENTAL":      SyncTableModeIncremental,
		"initialization":   SyncTableModeInitialization,
		"  Initialization": SyncTableModeInitialization,
		"initial":          SyncTableModeInitialization,
		"init":             SyncTableModeInitialization,
	}
	for in, want := range cases {
		if got := NormalizeSyncTableMode(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestTableMappingSyncModeAlias(t *testing.T) {
	var m TableMapping
	if err := json.Unmarshal([]byte(`{"target_table":"t","sync_mode":"initial"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.SyncTableMode != SyncTableModeInitialization {
		t.Fatalf("got %q", m.SyncTableMode)
	}
	var m2 TableMapping
	if err := json.Unmarshal([]byte(`{"target_table":"t","sync_table_mode":"incremental","sync_mode":"initial"}`), &m2); err != nil {
		t.Fatal(err)
	}
	if m2.SyncTableMode != SyncTableModeIncremental {
		t.Fatalf("canonical should win, got %q", m2.SyncTableMode)
	}
}

func TestBuildSnapshotDataCollections(t *testing.T) {
	got := BuildSnapshotDataCollections("public", []TableMapping{
		{SourceTables: []string{"a", "b"}, SyncTableMode: SyncTableModeInitialization},
		{SourceTables: []string{"a"}},
	})
	if len(got) != 2 || got[0] != "public.a" || got[1] != "public.b" {
		t.Fatalf("%v", got)
	}
}

func TestKafkaOffsetForMapping(t *testing.T) {
	if KafkaOffsetForMapping("init", TableMapping{}) != "OFFSET_BEGINNING" {
		t.Fatal("init")
	}
	if KafkaOffsetForMapping("add_table", TableMapping{SyncTableMode: "incremental"}) != "OFFSET_END" {
		t.Fatal("incremental")
	}
	if KafkaOffsetForMapping("add_table", TableMapping{SyncTableMode: "initialization"}) != "OFFSET_BEGINNING" {
		t.Fatal("initialization")
	}
}
