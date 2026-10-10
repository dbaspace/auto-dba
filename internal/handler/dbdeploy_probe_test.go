package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestValidateRegisterNodes(t *testing.T) {
	ok := []struct {
		topo  string
		nodes []store.DbDeployNode
	}{
		{store.DbDeployTopoStandalone, []store.DbDeployNode{{Role: "primary", Host: "a", Port: 3306}}},
		{store.DbDeployTopoStandalone, []store.DbDeployNode{{Role: "single", Host: "a", Port: 3306}}},
		{store.DbDeployTopoMasterReplica, []store.DbDeployNode{{Role: "primary", Host: "a", Port: 3306}, {Role: "replica", Host: "b", Port: 3306}}},
	}
	for _, c := range ok {
		if err := validateRegisterNodes(c.topo, c.nodes); err != nil {
			t.Errorf("%s: unexpected %v", c.topo, err)
		}
	}
	bad := []struct {
		topo  string
		nodes []store.DbDeployNode
	}{
		{store.DbDeployTopoStandalone, []store.DbDeployNode{{Role: "primary", Host: "a", Port: 1}, {Role: "primary", Host: "b", Port: 1}}},
		{store.DbDeployTopoStandalone, []store.DbDeployNode{{Role: "primary", Host: "a", Port: 1}, {Role: "replica", Host: "b", Port: 1}}},
		{store.DbDeployTopoStandalone, []store.DbDeployNode{{Role: "primary", Host: "a"}}},
		{store.DbDeployTopoMasterReplica, []store.DbDeployNode{{Role: "primary", Host: "a", Port: 1}}},
		{store.DbDeployTopoMasterReplica, []store.DbDeployNode{{Role: "replica", Host: "a"}, {Role: "replica", Host: "b"}}},
		{store.DbDeployTopoMasterReplica, []store.DbDeployNode{{Role: "primary", Host: "a"}, {Role: "primary", Host: "b"}}},
		{store.DbDeployTopoMasterReplica, []store.DbDeployNode{{Role: "primary", Host: "a", Port: 1}, {Role: "replica", Host: "A", Port: 1}}},
		{store.DbDeployTopoMasterReplica, []store.DbDeployNode{{Role: "primary", Host: " "}}},
	}
	for i, c := range bad {
		if err := validateRegisterNodes(c.topo, c.nodes); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestAgentIDForHost(t *testing.T) {
	agents := []store.DbDeployAgent{{ID: "a1", BaseURL: "http://10.0.0.1:8092"}, {ID: "a2", BaseURL: "http://db2.local:8092/"}}
	if got := agentIDForHost(agents, "10.0.0.1"); got != "a1" {
		t.Errorf("got %q", got)
	}
	if got := agentIDForHost(agents, "DB2.local"); got != "a2" {
		t.Errorf("got %q", got)
	}
	if got := agentIDForHost(agents, "10.0.0.9"); got != "" {
		t.Errorf("got %q", got)
	}
}
