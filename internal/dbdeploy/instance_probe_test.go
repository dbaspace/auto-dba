package dbdeploy

import (
	"testing"

	"auto-dba/internal/store"
)

func TestNormalizeProbedVersion(t *testing.T) {
	cases := map[[2]string]string{
		{store.DbDeployEngineMySQL, "8.0.36-log"}:                        "8.0.36",
		{store.DbDeployEngineMySQL, "8.0.36-28"}:                         "8.0.36",
		{store.DbDeployEngineMySQL, "5.7.44-log"}:                        "5.7.44",
		{store.DbDeployEnginePostgres, "16.4 (Debian 16.4-1.pgdg120+1)"}: "16.4",
		{store.DbDeployEnginePostgres, "17beta1"}:                        "17",
	}
	for in, want := range cases {
		if got := NormalizeProbedVersion(in[0], in[1]); got != want {
			t.Errorf("NormalizeProbedVersion(%s,%q)=%q want %q", in[0], in[1], got, want)
		}
	}
}

func TestInstanceAddrs(t *testing.T) {
	got := InstanceAddrs(&store.Instance{Host: "10.0.0.1, 10.0.0.2:3307，db.example.com", Port: 3306})
	want := []ProbeNode{{Host: "10.0.0.1", Port: 3306}, {Host: "10.0.0.2", Port: 3307}, {Host: "db.example.com", Port: 3306}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestMergeProbeNodes(t *testing.T) {
	self := ProbeNode{Host: "10.0.0.1", Port: 3306}

	nodes, topo, _ := MergeProbeNodes(self, false, nil, nil, nil)
	if topo != store.DbDeployTopoStandalone || len(nodes) != 1 || nodes[0].Role != "primary" {
		t.Fatalf("standalone: %s %+v", topo, nodes)
	}

	// 同机从库端口未知（0）时不能被并入当前节点
	nodes, topo, warns := MergeProbeNodes(self, false, nil, []ProbeNode{{Host: "10.0.0.1"}, {Host: "10.0.0.4"}}, nil)
	if topo != store.DbDeployTopoMasterReplica || len(nodes) != 3 || nodes[0].Role != "primary" ||
		nodes[1].Role != "replica" || nodes[1].Port != 0 || nodes[2].Port != 0 || len(warns) != 2 {
		t.Fatalf("unknown replica port: %s %+v %v", topo, nodes, warns)
	}

	nodes, topo, _ = MergeProbeNodes(self, false, nil,
		[]ProbeNode{{Host: "10.0.0.2", Port: 3306}, {Host: "10.0.0.2", Port: 3306}},
		[]ProbeNode{{Host: "10.0.0.2", Port: 3306}})
	if topo != store.DbDeployTopoMasterReplica || len(nodes) != 2 || nodes[0].Role != "primary" || nodes[1].Role != "replica" {
		t.Fatalf("primary dedup: %s %+v", topo, nodes)
	}

	nodes, topo, _ = MergeProbeNodes(self, true, &ProbeNode{Host: "10.0.0.9", Port: 3306}, nil, nil)
	if topo != store.DbDeployTopoMasterReplica || len(nodes) != 2 || nodes[0].Role != "primary" || nodes[0].Host != "10.0.0.9" || nodes[1].Role != "replica" {
		t.Fatalf("replica: %s %+v", topo, nodes)
	}

	nodes, topo, warns = MergeProbeNodes(self, false, nil, nil, []ProbeNode{{Host: "10.0.0.3", Port: 3306}})
	if topo != store.DbDeployTopoMasterReplica || len(nodes) != 2 || nodes[0].Role != "primary" || nodes[1].Source != "instance" || len(warns) != 2 {
		t.Fatalf("extra hosts: %s %+v %v", topo, nodes, warns)
	}
}

func TestPgBaseFromBinDir(t *testing.T) {
	cases := map[string]string{
		"/usr/lib/postgresql/15/bin": "/usr/lib/postgresql/15",
		"/usr/pgsql-16/bin/":         "/usr/pgsql-16",
		"/opt/pg15/bin":              "/opt/pg15",
		"":                           "",
		"bin":                        "",
	}
	for in, want := range cases {
		if got := pgBaseFromBinDir(in); got != want {
			t.Errorf("pgBaseFromBinDir(%q)=%q want %q", in, got, want)
		}
	}
}

func TestPgBaseFromLayout(t *testing.T) {
	cases := []struct{ cfg, data, want string }{
		{"/etc/postgresql/15/main/postgresql.conf", "/var/lib/postgresql/15/main", "/usr/lib/postgresql/15"},
		{"/var/lib/pgsql/16/data/postgresql.conf", "/var/lib/pgsql/16/data", "/usr/pgsql-16"},
		{"", "/var/lib/pgsql/14/data", "/usr/pgsql-14"},
		{"/home/postgres/data/postgresql.conf", "/home/postgres/data", ""},
	}
	for _, c := range cases {
		if got := pgBaseFromLayout(c.cfg, c.data); got != c.want {
			t.Errorf("pgBaseFromLayout(%q,%q)=%q want %q", c.cfg, c.data, got, c.want)
		}
	}
}
