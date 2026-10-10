package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestPickLinkedInstance(t *testing.T) {
	cl := &store.SrCluster{ID: "c1", FeHost: "10.100.112.14", FeQueryPort: 9030}
	fe := []string{"10.100.112.14"}
	resolve := func(h string) []string {
		if h == "dorisdb.test.restosuite.ai" {
			return []string{"10.100.112.14"}
		}
		return nil
	}
	insts := []store.Instance{
		{ID: "mysql", DBType: "mysql", Host: "10.100.112.14", Port: 9030, Password: "x"},
		{ID: "other-port", DBType: "starrocks", Host: "10.100.112.14", Port: 9031, Password: "x"},
		{ID: "nopass", DBType: "starrocks", Host: "10.100.112.14", Port: 9030},
		{ID: "test-sr", DBType: "starrocks", Host: "dorisdb.test.restosuite.ai", Port: 9030, Password: "enc"},
	}
	it, by := pickLinkedInstance(insts, cl, fe, resolve)
	if it == nil || it.ID != "test-sr" || by != "host" {
		t.Fatalf("按域名解析匹配且优先有密码: %+v %s", it, by)
	}

	insts = append(insts, store.Instance{ID: "tagged", DBType: "StarRocks", Host: "vip.example", Port: 1, Tags: []string{"sr-cluster=c1"}})
	it, by = pickLinkedInstance(insts, cl, fe, resolve)
	if it == nil || it.ID != "tagged" || by != "tag" {
		t.Fatalf("标签优先: %+v %s", it, by)
	}

	if it, _ := pickLinkedInstance(insts[:3], &store.SrCluster{ID: "c2", FeHost: "10.0.0.9", FeQueryPort: 9030}, []string{"10.0.0.9"}, resolve); it != nil {
		t.Fatalf("主机不匹配不应关联: %+v", it)
	}

	cross := []store.Instance{
		{ID: "other-ten", TenantID: "t-b", DBType: "starrocks", Host: "10.100.112.14", Port: 9030, Password: "x", Tags: []string{"sr-cluster=c1"}},
		{ID: "same-ten", TenantID: "t-a", DBType: "starrocks", Host: "10.100.112.14", Port: 9030, Password: "x"},
	}
	it, by = pickLinkedInstance(cross, &store.SrCluster{ID: "c1", TenantID: "t-a", FeHost: "10.100.112.14", FeQueryPort: 9030}, fe, nil)
	if it == nil || it.ID != "same-ten" || by != "host" {
		t.Fatalf("跨租户标签不得命中: %+v %s", it, by)
	}
}

func TestInstanceHitsMultiHost(t *testing.T) {
	if !instanceHitsHosts("10.0.0.1, 10.0.0.2", []string{"10.0.0.2"}, nil) {
		t.Fatal("多地址任一命中即可")
	}
}

func TestAlignRegisteredFERoles(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "a", Component: "fe", Host: "10.0.0.1", FeRole: "follower"},
		{ID: "b", Component: "fe", Host: "10.0.0.2", FeRole: "follower"},
		{ID: "c", Component: "fe", Host: "10.0.0.3", FeRole: "observer"},
		{ID: "d", Component: "be", Host: "10.0.0.1"},
	}
	rows := []procVersionRow{
		{Host: "10.0.0.1", Role: "LEADER"},
		{Host: "10.0.0.2", Role: "OBSERVER"},
		{Host: "10.0.0.9", Role: "FOLLOWER"},
	}
	diffs := alignRegisteredFERoles(nodes, rows)
	if nodes[0].FeRole != "follower" || nodes[1].FeRole != "observer" || nodes[2].FeRole != "observer" {
		t.Fatalf("roles=%s,%s,%s", nodes[0].FeRole, nodes[1].FeRole, nodes[2].FeRole)
	}
	joined := strings.Join(diffs, "；")
	if !strings.Contains(joined, "FE 10.0.0.3 未出现") || !strings.Contains(joined, "10.0.0.9（FOLLOWER）未登记") {
		t.Fatalf("diffs=%q", joined)
	}
}
