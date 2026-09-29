package dbdeploy

import (
	"strings"
	"testing"
)

func TestAssertAllowDeploy(t *testing.T) {
	if err := AssertAllowDeploy("mysql"); err != nil {
		t.Fatal(err)
	}
	if err := AssertAllowDeploy("tidb"); err != nil {
		t.Fatal(err)
	}
	if err := AssertAllowDeploy("oracle"); err != nil {
		t.Fatal(err)
	}
	if err := AssertAllowDeploy("oceanbase"); err != nil {
		t.Fatal(err)
	}
	if err := AssertAllowDeploy("sqlserver"); err != nil {
		t.Fatal(err)
	}
	if err := AssertAllowDeploy("starrocks"); err != nil {
		t.Fatal(err)
	}
	if err := AssertAllowDeploy("unknown"); err == nil {
		t.Fatal("unknown should fail")
	}
}

func TestValidateClusterAgainstRegistry(t *testing.T) {
	if err := ValidateClusterAgainstRegistry("mysql", "self", "standalone", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("tidb", "self", "pd_tikv_tidb", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("tidb", "cloud", "cloud_endpoint", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("cockroachdb", "self", "multi_node", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("cassandra", "cloud", "cloud_endpoint", false); err == nil {
		t.Fatal("cassandra cloud should fail")
	}
	if err := ValidateClusterAgainstRegistry("kafka", "self", "broker_cluster", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("kafka", "self", "master_replica", false); err == nil {
		t.Fatal("kafka master_replica should fail")
	}
	if err := ValidateClusterAgainstRegistry("clickhouse", "self", "master_replica", false); err != nil {
		t.Fatal(err)
	}
	if err := AssertAllowDeploy("mongodb"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("oracle", "self", "standalone", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("oracle", "self", "master_replica", false); err == nil {
		t.Fatal("oracle master_replica should fail")
	}
	if err := ValidateClusterAgainstRegistry("oceanbase", "self", "master_replica", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("sqlserver", "self", "master_replica", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateClusterAgainstRegistry("starrocks", "self", "standalone", false); err == nil {
		t.Fatal("starrocks via dbdeploy should fail")
	}
}

func TestDiffAgainstInstanceTypes(t *testing.T) {
	warns := DiffAgainstInstanceTypes([]string{"mysql", "host", "weird_engine"})
	found := false
	for _, w := range warns {
		if strings.Contains(w, "weird_engine") {
			found = true
		}
		if strings.Contains(w, "host") {
			t.Fatal("host must be ignored")
		}
	}
	if !found {
		t.Fatalf("expected weird_engine warning, got %v", warns)
	}
}

func TestListEnginesForAPI_NoHost(t *testing.T) {
	list := ListEnginesForAPI()
	if len(list) < 10 {
		t.Fatalf("expected many engines, got %d", len(list))
	}
	for _, e := range list {
		id, _ := e["id"].(string)
		if id == "host" {
			t.Fatal("host must not appear in deploy registry")
		}
		kind, _ := e["agent_kind"].(string)
		port, _ := e["agent_port"].(int)
		switch id {
		case "haproxy", "nginx", "lvs":
			if kind != "platform" || port != 8090 {
				t.Fatalf("%s want platform:8090, got %s:%v", id, kind, port)
			}
		case "starrocks":
			if kind != "node" || port != 8091 {
				t.Fatalf("starrocks want node:8091, got %s:%v", kind, port)
			}
		case "mysql", "postgres", "redis":
			if kind != "db" || port != 8092 {
				t.Fatalf("%s want db:8092, got %s:%v", id, kind, port)
			}
		}
	}
	ags := AgentsOverview()
	if len(ags) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(ags))
	}
}
