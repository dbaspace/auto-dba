package dbdeploy

import (
	"testing"

	"auto-dba/internal/store"
)

func TestValidateKafkaBrokerCluster(t *testing.T) {
	e := &Engine{}
	cl := &store.DbDeployCluster{Engine: "kafka", Topology: TopoBrokerCluster}
	err := e.validateTopology(cl, []store.DbDeployNode{{Role: "broker"}})
	if err != nil {
		t.Fatal(err)
	}
	err = e.validateTopology(cl, []store.DbDeployNode{{Role: "endpoint"}})
	if err == nil {
		t.Fatal("endpoint-only should fail")
	}
}

func TestBuildStepsClickHouse(t *testing.T) {
	e := &Engine{}
	cl := &store.DbDeployCluster{Engine: "clickhouse", Topology: store.DbDeployTopoStandalone}
	steps := e.buildDeploySteps(cl, []store.DbDeployNode{{ID: "1", Role: "single", Host: "a"}})
	found := false
	for _, s := range steps {
		if s.StepKey == "component_health" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected component_health")
	}
}

func TestOrderCassandraSeedFirst(t *testing.T) {
	cl := &store.DbDeployCluster{Engine: "cassandra", Topology: TopoMultiNode}
	out := orderNodesForDeploy(cl, []store.DbDeployNode{
		{Role: "node", Host: "b"},
		{Role: "seed", Host: "a"},
	})
	if out[0].Role != "seed" {
		t.Fatalf("seed should be first, got %s", out[0].Role)
	}
}
