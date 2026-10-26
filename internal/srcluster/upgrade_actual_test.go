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

func TestNodeLiveMapping(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "fe1", Component: "fe", Host: "10.100.113.151", FeRole: "follower"},
		{ID: "fe2", Component: "fe", Host: "10.100.113.152", FeRole: "observer"},
		{ID: "be1", Component: "be", Host: "10.100.113.151"},
		{ID: "fe3", Component: "fe", Host: "10.100.113.199"},
	}
	fe := []procVersionRow{
		{Host: "10.100.113.151", Role: "LEADER", Alive: "true", Version: "3.3.22-x"},
		{Host: "10.100.113.152", Role: "FOLLOWER", Alive: "false"},
	}
	be := []procVersionRow{{Host: "10.100.113.151", Role: "", Alive: "true", Version: "3.3.22-x"}}
	live := mapNodeLive(nodes, fe, be)
	if live["fe1"].Role != "LEADER" || live["fe1"].Alive != "true" {
		t.Fatalf("fe1=%+v", live["fe1"])
	}
	if live["fe2"].Role != "FOLLOWER" || live["fe2"].Alive != "false" {
		t.Fatalf("fe2=%+v", live["fe2"])
	}
	if live["be1"].Role != "" || live["be1"].Version != "3.3.22-x" {
		t.Fatalf("be1 不应带角色: %+v", live["be1"])
	}
	if _, ok := live["fe3"]; ok {
		t.Fatalf("未上报 FE 不应有实时信息")
	}
}

func TestNodeLiveBackendResources(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "fe1", Component: "fe", Host: "10.0.0.1"},
		{ID: "be1", Component: "be", Host: "10.0.0.1"},
	}
	res := procVersionRow{Host: "10.0.0.1", Alive: "true", TabletNum: "69",
		ReplayedJournalID: "12345", LastHeartbeat: "2026-10-09 10:31:49", StartTime: "2026-10-08 16:18:01", CpuCores: "4", TotalCapacity: "99.951 GB", AvailCapacity: "62.949 GB", DataUsed: "0.000 B", UsedPct: "37.02 %"}
	live := mapNodeLive(nodes, []procVersionRow{res}, []procVersionRow{res})
	be := live["be1"]
	if be.TabletNum != "69" || be.CpuCores != "4" || be.TotalCapacity != "99.951 GB" || be.AvailCapacity != "62.949 GB" || be.DataUsed != "0.000 B" || be.UsedPct != "37.02 %" {
		t.Fatalf("be1=%+v", be)
	}
	fe := live["fe1"]
	if be.ReplayedJournalID != "" {
		t.Fatalf("BE 不应带 FE 日志: %+v", be)
	}
	if be.LastHeartbeat != "2026-10-09 10:31:49" || be.StartTime != "2026-10-08 16:18:01" {
		t.Fatalf("BE 应带 LastHeartbeat/LastStartTime: %+v", be)
	}
	if fe.ReplayedJournalID != "12345" || fe.LastHeartbeat != "2026-10-09 10:31:49" || fe.StartTime != "2026-10-08 16:18:01" {
		t.Fatalf("fe1=%+v", fe)
	}
	if fe.TabletNum != "" || fe.CpuCores != "" || fe.TotalCapacity != "" || fe.AvailCapacity != "" || fe.DataUsed != "" || fe.UsedPct != "" {
		t.Fatalf("FE 不应带资源: %+v", fe)
	}
}

func TestWithoutNodes(t *testing.T) {
	nodes := []store.SrNode{{ID: "fe1"}, {ID: "be1"}, {ID: "be2"}}
	got := withoutNodes(nodes, []store.SrNode{{ID: "be1"}})
	if len(got) != 2 || got[0].ID != "fe1" || got[1].ID != "be2" {
		t.Fatalf("got %+v", got)
	}
}

func TestBackendResourceCols(t *testing.T) {
	// 3.3.22 实际列序
	v33 := []string{"BackendId", "IP", "HeartbeatPort", "BePort", "HttpPort", "BrpcPort", "LastStartTime", "LastHeartbeat", "Alive",
		"SystemDecommissioned", "ClusterDecommissioned", "TabletNum", "DataUsedCapacity", "AvailCapacity", "TotalCapacity", "UsedPct",
		"MaxDiskUsedPct", "ErrMsg", "Version", "Status", "DataTotalCapacity", "DataUsedPct", "CpuCores", "MemLimit"}
	if got := backendResourceCols(v33); got != (backendResourceIdx{cpu: 22, total: 14, avail: 13, dataUsed: 12, usedPct: 15}) {
		t.Fatalf("3.3 列: %+v", got)
	}
	if got := backendResourceCols([]string{"BackendId", "IP", "Alive"}); got != (backendResourceIdx{-1, -1, -1, -1, -1}) {
		t.Fatalf("缺列: %+v", got)
	}
}

func TestProcStartTimeIdx(t *testing.T) {
	if got := procStartTimeIdx(map[string]int{"starttime": 3, "laststarttime": 8}); got != 3 {
		t.Fatalf("FE StartTime 优先: %d", got)
	}
	if got := procStartTimeIdx(map[string]int{"laststarttime": 6}); got != 6 {
		t.Fatalf("BE LastStartTime: %d", got)
	}
	if got := procStartTimeIdx(map[string]int{}); got != -1 {
		t.Fatalf("缺列: %d", got)
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
