package cdc

import (
	"encoding/json"
	"testing"
)

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
