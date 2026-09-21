package cdc

import "testing"

func TestParseTaskExtraKeepsMappingObjects(t *testing.T) {
	desc := `-- auto-dba-cdc:{"tables":["a","b"],"mappings":[{"target_table":"t1","source_tables":["a"]}],"shard_db":true}`
	got := parseTaskExtra(desc)
	if got["tables"] != "a,b" {
		t.Fatalf("tables=%q", got["tables"])
	}
	if got["shard_db"] != "true" {
		t.Fatalf("shard_db=%q", got["shard_db"])
	}
	if got["mappings"] == "" || got["mappings"] == "," {
		t.Fatalf("mappings should stay JSON, got %q", got["mappings"])
	}
	maps := ParseMappingsJSON(got["mappings"])
	if len(maps) != 1 || maps[0].TargetTable != "t1" {
		t.Fatalf("%+v", maps)
	}
}
