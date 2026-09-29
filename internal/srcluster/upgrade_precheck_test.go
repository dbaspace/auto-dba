package srcluster

import "testing"

func TestPrecheckUpgradeNilEngine(t *testing.T) {
	var e *Engine
	res := e.PrecheckUpgrade(nil, "x", "y")
	if res.OK {
		t.Fatal("expected fail")
	}
}

func TestPrecheckUpgradeMissingRelease(t *testing.T) {
	res := (&Engine{}).PrecheckUpgrade(nil, "cid", "")
	if res.OK {
		t.Fatal("expected fail empty release")
	}
	found := false
	for _, it := range res.Items {
		if it.Name == "target_release" && !it.OK {
			found = true
		}
	}
	if !found {
		t.Fatalf("items=%v", res.Items)
	}
}
