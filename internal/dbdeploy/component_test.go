package dbdeploy

import (
	"testing"

	"auto-dba/internal/store"
)

func TestValidateTiDBTopology(t *testing.T) {
	e := &Engine{}
	cl := &store.DbDeployCluster{Engine: "tidb", Topology: TopoPdTikvTidb}
	err := e.validateTopology(cl, []store.DbDeployNode{{Role: "pd"}, {Role: "tikv"}})
	if err == nil {
		t.Fatal("missing tidb role should fail")
	}
	err = e.validateTopology(cl, []store.DbDeployNode{{Role: "pd"}, {Role: "tikv"}, {Role: "tidb"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOrderTiDBNodes(t *testing.T) {
	cl := &store.DbDeployCluster{Engine: "tidb", Topology: TopoPdTikvTidb}
	out := orderNodesForDeploy(cl, []store.DbDeployNode{
		{Role: "tidb", Host: "c"},
		{Role: "pd", Host: "a"},
		{Role: "tikv", Host: "b"},
	})
	if out[0].Role != "pd" || out[1].Role != "tikv" || out[2].Role != "tidb" {
		t.Fatalf("unexpected order: %+v", out)
	}
}

func TestBuildDeployStepsComponentHealth(t *testing.T) {
	e := &Engine{}
	cl := &store.DbDeployCluster{Engine: "tidb", Topology: TopoPdTikvTidb}
	nodes := []store.DbDeployNode{{ID: "1", Role: "pd", Host: "a"}, {ID: "2", Role: "tikv", Host: "b"}, {ID: "3", Role: "tidb", Host: "c"}}
	steps := e.buildDeploySteps(cl, nodes)
	found := false
	for _, s := range steps {
		if s.StepKey == "component_health" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected component_health step")
	}
}
