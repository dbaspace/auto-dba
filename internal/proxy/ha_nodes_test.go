package proxy

import "testing"

func TestValidateAndNormalizeHANodes_Pair(t *testing.T) {
	nodes, topo, err := ValidateAndNormalizeHANodes("haproxy", []HANodeSpec{
		{Role: "master", AgentID: "a1"},
		{Role: "backup", AgentID: "a2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if topo != TopologyHAPair {
		t.Fatalf("topo=%s", topo)
	}
	if nodes[0].Priority <= nodes[1].Priority {
		t.Fatalf("master priority should be higher: %+v", nodes)
	}
}

func TestValidateAndNormalizeHANodes_NginxMulti(t *testing.T) {
	nodes, topo, err := ValidateAndNormalizeHANodes("nginx", []HANodeSpec{
		{AgentID: "a1"},
		{AgentID: "a2"},
		{AgentID: "a3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if topo != TopologyHAMulti || len(nodes) != 3 {
		t.Fatalf("got topo=%s n=%d", topo, len(nodes))
	}
	m := 0
	for _, n := range nodes {
		if n.Role == RoleMaster {
			m++
		}
	}
	if m != 1 {
		t.Fatalf("want 1 master, got %d", m)
	}
}

func TestValidateAndNormalizeHANodes_RejectSingle(t *testing.T) {
	_, _, err := ValidateAndNormalizeHANodes("lvs", []HANodeSpec{{AgentID: "a1"}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateAndNormalizeHANodes_HaproxyReject3(t *testing.T) {
	_, _, err := ValidateAndNormalizeHANodes("haproxy", []HANodeSpec{
		{AgentID: "a1"}, {AgentID: "a2"}, {AgentID: "a3"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildKeepalivedConfForNode(t *testing.T) {
	shared := &LVSOptions{Interface: "eth0", VirtualRouterID: 60, AuthPass: "pass1234"}
	master := HANodeSpec{Role: RoleMaster, Priority: 150, AgentID: "a1"}
	backup := HANodeSpec{Role: RoleBackup, Priority: 100, AgentID: "a2"}
	m, err := BuildKeepalivedConfForNode("haproxy", "demo", "10.0.0.9", 3306, nil, shared, master)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildKeepalivedConfForNode("haproxy", "demo", "10.0.0.9", 3306, nil, shared, backup)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(m, "state MASTER") || !contains(m, "priority 150") {
		t.Fatalf("master conf:\n%s", m)
	}
	if !contains(b, "state BACKUP") || !contains(b, "priority 100") || !contains(b, "nopreempt") {
		t.Fatalf("backup conf:\n%s", b)
	}
}
