package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestBERemoveStepsDoNotStopNode(t *testing.T) {
	for _, force := range []bool{false, true} {
		steps := beRemoveSteps("n1", force)
		for _, s := range steps {
			if strings.HasPrefix(s[0], "stop_node_") || s[0] == "verify_backends" {
				t.Fatalf("force=%v 不应含 %s", force, s[0])
			}
		}
		want := "decommission_be_n1"
		if force {
			want = "drop_backend_n1"
		}
		if steps[0][0] != want {
			t.Fatalf("force=%v first=%s", force, steps[0][0])
		}
	}
}

func TestActiveNodesExcludesDecommissioning(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "fe1", Component: "fe", Status: store.SrNodeReady},
		{ID: "be1", Component: "be", Status: store.SrNodeDecommissioning},
		{ID: "be2", Component: "be", Status: store.SrNodeStopped},
	}
	got := activeNodes(nodes)
	if len(got) != 2 || got[0].ID != "fe1" || got[1].ID != "be2" {
		t.Fatalf("got=%v", got)
	}
}

func TestNodeLiveDecommissioned(t *testing.T) {
	nodes := []store.SrNode{{ID: "be1", Component: "be", Host: "10.0.0.1"}, {ID: "fe1", Component: "fe", Host: "10.0.0.1"}}
	fe := []procVersionRow{{Host: "10.0.0.1", Role: "LEADER", Alive: "true", Decommissioned: true}}
	be := []procVersionRow{{Host: "10.0.0.1", Alive: "true", Decommissioned: true, TabletNum: "128"}}
	live := mapNodeLive(nodes, fe, be)
	if !live["be1"].Decommissioned || live["be1"].TabletNum != "128" {
		t.Fatalf("be1=%+v", live["be1"])
	}
	if live["fe1"].Decommissioned {
		t.Fatalf("FE 不应带下线标记")
	}
}

func TestPickRemovedBackends(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "be1", Component: "be", Host: "10.0.0.1"},
		{ID: "be2", Component: "be", Host: "10.0.0.2"},
		{ID: "be3", Component: "be", Host: "10.0.0.3"},
		{ID: "be4", Component: "be", Host: "10.0.0.4"},
		{ID: "fe1", Component: "fe", Host: "10.0.0.9"},
	}
	rows := []procVersionRow{{Host: "10.0.0.2", Decommissioned: true}, {Host: "10.0.0.9"}}
	changes := []changeNodes{
		// be3 最近一次是恢复成功，后来即使不在 backends 也不能删
		{ID: "c5", Type: store.SrChangeRecommissionBE, Status: store.SrChangeSuccess, NodeIDs: []string{"be3"}},
		{ID: "c4", Type: store.SrChangeRestart, Status: store.SrChangeSuccess, NodeIDs: []string{"be1"}},
		{ID: "c3", Type: store.SrChangeDecommissionBE, Status: store.SrChangeSuccess, NodeIDs: []string{"be1"}},
		{ID: "c2", Type: store.SrChangeDecommissionBE, Status: store.SrChangeSuccess, NodeIDs: []string{"be2"}},
		{ID: "c1", Type: store.SrChangeDecommissionBE, Status: store.SrChangeFailed, NodeIDs: []string{"be3", "be4"}},
	}
	got := pickRemovedBackends(nodes, changes, rows, "")
	if len(got) != 1 || got["be1"] != "c3" {
		t.Fatalf("仅已迁移完成的 be1 可删: %v", got)
	}

	running := append([]changeNodes{{ID: "c6", Type: store.SrChangeUpgrade, Status: store.SrChangeRunning}}, changes...)
	if got := pickRemovedBackends(nodes, running, rows, ""); len(got) != 0 {
		t.Fatalf("有进行中变更时不删: %v", got)
	}

	current := append([]changeNodes{{ID: "c7", Type: store.SrChangeRemoveBE, Status: store.SrChangeRunning, NodeIDs: []string{"be4"}}}, changes...)
	got = pickRemovedBackends(nodes, current, rows, "c7")
	if got["be4"] != "c7" || got["be1"] != "c3" {
		t.Fatalf("当前收尾的 DROP 视为成功: %v", got)
	}

	if got := pickRemovedBackends(nodes, changes, nil, ""); len(got) != 0 {
		t.Fatalf("SHOW PROC 无行时不删: %v", got)
	}
}
