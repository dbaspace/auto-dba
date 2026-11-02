package handler

import "testing"

func TestCdcBatchAddTableMaxParentsCoversShardFanout(t *testing.T) {
	// 4 实例 × 16 库 = 64；上限需覆盖并留余量
	if cdcBatchAddTableMaxParents < 64 {
		t.Fatalf("cdcBatchAddTableMaxParents=%d, want >= 64", cdcBatchAddTableMaxParents)
	}
	if cdcBatchAddTableMaxParents < 128 {
		t.Fatalf("cdcBatchAddTableMaxParents=%d, want 128", cdcBatchAddTableMaxParents)
	}
}

func TestSubtractFoldKeepsCrossParentTables(t *testing.T) {
	kept := subtractFold([]string{"orders", "items"}, []string{"orders"})
	if len(kept) != 1 || kept[0] != "items" {
		t.Fatalf("kept=%v", kept)
	}
}
