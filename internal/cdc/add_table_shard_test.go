package cdc

import "testing"

func TestSplitAddTableMapsForProvisionReusesParentTarget(t *testing.T) {
	parent := []TableMapping{
		{SourceTables: []string{"t_0", "t_1"}, TargetTable: "t", IsShardGroup: true},
	}
	add := []TableMapping{
		{SourceTables: []string{"t_2"}, TargetTable: "t", IsShardGroup: true},
		{SourceTables: []string{"orders"}, TargetTable: "orders"},
	}
	need, reused := splitAddTableMapsForProvision(add, parent, nil)
	if len(reused) != 1 || reused[0] != "t" {
		t.Fatalf("reused=%v", reused)
	}
	if len(need) != 1 || need[0].TargetTable != "orders" {
		t.Fatalf("need=%v", need)
	}
}

func TestSplitAddTableMapsForProvisionPlainTables(t *testing.T) {
	need, reused := splitAddTableMapsForProvision(nil, nil, []string{"a", "b"})
	if len(reused) != 0 || len(need) != 2 {
		t.Fatalf("need=%d reused=%d", len(need), len(reused))
	}
}
