package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestMapNodeVersionsAndMismatch(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "fe1", Component: "fe", Host: "10.100.113.151"},
		{ID: "be1", Component: "be", Host: "10.100.113.151"},
		{ID: "be2", Component: "be", Host: "10.100.112.17"},
		{ID: "be3", Component: "be", Host: "10.100.112.99"},
	}
	fe := []procVersionRow{{Host: "10.100.113.151", Version: "3.2.10-f61f51a"}}
	be := []procVersionRow{
		{Host: "10.100.113.151", Version: "3.3.22-3b8e4b8"},
		{Host: "10.100.112.17", Version: "3.3.22-3b8e4b8"},
	}
	actual := mapNodeVersions(nodes, fe, be)
	if actual["fe1"] != "3.2.10-f61f51a" || actual["be1"] != "3.3.22-3b8e4b8" {
		t.Fatalf("同主机 FE/BE 须按组件区分: %v", actual)
	}
	if _, ok := actual["be3"]; ok {
		t.Fatalf("未上报节点不应有版本")
	}
	bad, desc := mismatchedNodes(nodes, actual, "3.3.22")
	if len(bad) != 2 || bad[0].ID != "fe1" || bad[1].ID != "be3" {
		t.Fatalf("bad=%v", bad)
	}
	joined := strings.Join(desc, "；")
	if !strings.Contains(joined, "FE 10.100.113.151=3.2.10-f61f51a") || !strings.Contains(joined, "BE 10.100.112.99=未上报") {
		t.Fatalf("desc=%q", joined)
	}
	if bad, _ := mismatchedNodes(nodes[1:3], actual, "3.3.22"); len(bad) != 0 {
		t.Fatalf("已是目标版本的节点不应补升级: %v", bad)
	}
}

func TestReleaseIDForVersion(t *testing.T) {
	rels := []store.SrRelease{{ID: "r1", Version: "3.2.1"}, {ID: "r2", Version: "3.2.10"}, {ID: "r3", Version: "3.3.22"}}
	if got := releaseIDForVersion(rels, "3.2.10-f61f51a"); got != "r2" {
		t.Fatalf("got %q", got)
	}
	if got := releaseIDForVersion(rels, "2.5.0-x"); got != "" {
		t.Fatalf("got %q", got)
	}
}
