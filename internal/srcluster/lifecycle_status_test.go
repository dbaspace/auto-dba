package srcluster

import (
	"testing"

	"auto-dba/internal/store"
)

func TestChangeUpdatesClusterLifecycle(t *testing.T) {
	t.Parallel()
	yes := []string{store.SrChangeClusterInit, store.SrChangeClusterReinit}
	no := []string{
		store.SrChangeStopNode,
		store.SrChangeStartNode,
		store.SrChangeRestart,
		store.SrChangeUpgrade,
		store.SrChangeUpgradeRollback,
		store.SrChangeAddFE,
		store.SrChangeAddBE,
		store.SrChangeRemoveFE,
		store.SrChangeRemoveBE,
		store.SrChangeDecommissionBE,
		store.SrChangeRecommissionBE,
		store.SrChangeConfigApply,
		store.SrChangeConfigRollback,
		"",
		"unknown",
	}
	for _, typ := range yes {
		if !changeUpdatesClusterLifecycle(typ) {
			t.Errorf("changeUpdatesClusterLifecycle(%q) = false, want true", typ)
		}
	}
	for _, typ := range no {
		if changeUpdatesClusterLifecycle(typ) {
			t.Errorf("changeUpdatesClusterLifecycle(%q) = true, want false", typ)
		}
	}
}
