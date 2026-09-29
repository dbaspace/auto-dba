package dbdeploy

import (
	"testing"

	"auto-dba/internal/store"
)

func TestOrderNodesForDeploy_Codis(t *testing.T) {
	cl := &store.DbDeployCluster{Engine: store.DbDeployEngineCodis, Topology: store.DbDeployTopoCodis}
	nodes := []store.DbDeployNode{
		{ID: "1", Role: "codis_proxy", Host: "p"},
		{ID: "2", Role: "codis_server", Host: "s"},
		{ID: "3", Role: "codis_dashboard", Host: "d"},
	}
	out := orderNodesForDeploy(cl, nodes)
	if out[0].Role != "codis_dashboard" || out[1].Role != "codis_server" || out[2].Role != "codis_proxy" {
		t.Fatalf("unexpected order: %+v", out)
	}
}

func TestValidateTopology_MySQLReplica(t *testing.T) {
	e := &Engine{}
	cl := &store.DbDeployCluster{Topology: store.DbDeployTopoMasterReplica}
	err := e.validateTopology(cl, []store.DbDeployNode{{Role: "primary", Host: "a"}})
	if err == nil {
		t.Fatal("expected replica required")
	}
	err = e.validateTopology(cl, []store.DbDeployNode{
		{Role: "primary", Host: "a"},
		{Role: "replica", Host: "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateTopology_Codis(t *testing.T) {
	e := &Engine{}
	cl := &store.DbDeployCluster{Topology: store.DbDeployTopoCodis}
	err := e.validateTopology(cl, []store.DbDeployNode{
		{Role: "codis_dashboard"}, {Role: "codis_proxy"},
	})
	if err == nil {
		t.Fatal("expected codis_server")
	}
}
