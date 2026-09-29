package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestBuildReinitPrepStepsOrder(t *testing.T) {
	cl := &store.SrCluster{SoftwareBase: "/home/dorisdb_base", DataRoot: "/data"}
	nodes := []store.SrNode{
		{ID: "fe1", Component: "fe", Host: "10.0.0.1", IsPrimaryFE: true},
		{ID: "fe2", Component: "fe", Host: "10.0.0.2", IsPrimaryFE: false},
		{ID: "be1", Component: "be", Host: "10.0.0.3"},
		{ID: "be2", Component: "be", Host: "10.0.0.4"},
	}
	steps := buildReinitPrepSteps(cl, nodes, "20260101-120000")
	if len(steps) < 1 || steps[0].StepKey != "agents_health" {
		t.Fatalf("first step want agents_health, got %#v", steps)
	}
	var keys []string
	for _, s := range steps {
		keys = append(keys, s.StepKey)
	}
	joined := strings.Join(keys, ",")
	// BE 先停，再非首 FE，再首 FE，再备份
	wantOrder := []string{
		"stop_node_be1", "stop_node_be2",
		"stop_node_fe2", "stop_node_fe1",
		"backup_dirs_fe1", "backup_dirs_fe2", "backup_dirs_be1", "backup_dirs_be2",
	}
	pos := 0
	for _, w := range wantOrder {
		idx := indexOf(keys, w)
		if idx < 0 {
			t.Fatalf("missing step %s in %s", w, joined)
		}
		if idx < pos {
			t.Fatalf("order broken for %s: %s", w, joined)
		}
		pos = idx
	}
}

func TestBuildReinitThenInitKeys(t *testing.T) {
	primary := &store.SrNode{ID: "fe1", Component: "fe", Host: "10.0.0.1", IsPrimaryFE: true}
	nodes := []store.SrNode{*primary, {ID: "be1", Component: "be", Host: "10.0.0.3"}}
	cl := &store.SrCluster{SoftwareBase: "/opt/sr", DataRoot: "/data"}
	steps := buildReinitPrepSteps(cl, nodes, "ts")
	steps = append(steps, buildInitSteps(nodes, primary)...)
	for i := range steps {
		steps[i].Seq = i + 1
	}
	if steps[len(steps)-1].StepKey != "finalize" {
		t.Fatalf("last=%s", steps[len(steps)-1].StepKey)
	}
	foundBootstrap := false
	for _, s := range steps {
		if s.StepKey == "bootstrap_fe_primary" {
			foundBootstrap = true
			break
		}
	}
	if !foundBootstrap {
		t.Fatal("missing bootstrap after reinit prep")
	}
	// seq 连续
	for i, s := range steps {
		if s.Seq != i+1 {
			t.Fatalf("seq gap at %d got %d", i, s.Seq)
		}
	}
}

func indexOf(ss []string, v string) int {
	for i, s := range ss {
		if s == v {
			return i
		}
	}
	return -1
}
