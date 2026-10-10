package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestMonitorEndpoints(t *testing.T) {
	cl := &store.SrCluster{FeHTTPPort: 8030}
	nodes := []store.SrNode{
		{Component: "fe", Host: "10.0.0.1"},
		{Component: "be", Host: "10.0.0.1"},
		{Component: "be", Host: "10.0.0.2", HTTPPort: 8041},
		{Component: "be", Host: "10.0.0.2", HTTPPort: 8041},
	}
	fe, be := monitorEndpoints(cl, nodes)
	if strings.Join(fe, ",") != "10.0.0.1:8030" || strings.Join(be, ",") != "10.0.0.1:8040,10.0.0.2:8041" {
		t.Fatalf("fe=%v be=%v", fe, be)
	}
}

func TestMergeMonitorJobCreate(t *testing.T) {
	cl := &store.SrCluster{ID: "c1", Name: "starrocks-etl", FeHTTPPort: 8030}
	job := &store.MonitorJob{Name: cl.Name}
	labels := map[string]string{"idc": "test", "version": "3.3.22", "domain": ""}
	mergeMonitorJob(job, cl, []string{"10.0.0.1:8030"}, []string{"10.0.0.1:8040", "10.0.0.2:8040"}, nil, labels)
	if len(job.TargetGroups) != 2 {
		t.Fatalf("groups=%+v", job.TargetGroups)
	}
	if job.TargetGroups[0].Labels["app"] != monitorAppFE || job.TargetGroups[1].Labels["app"] != monitorAppBE {
		t.Fatalf("app labels: %+v", job.TargetGroups)
	}
	if job.Collector != "test" || job.Labels["idc"] != "test" || !strings.Contains(job.Description, "sr-cluster=c1") {
		t.Fatalf("job=%+v", job)
	}
	sum := monitorSummary(job, cl)
	if sum.FECount != 1 || sum.BECount != 2 || strings.Join(sum.Missing, ",") != "domain" {
		t.Fatalf("summary=%+v", sum)
	}
}

func TestMergeMonitorJobExisting(t *testing.T) {
	cl := &store.SrCluster{ID: "c1", Name: "starrocks-test", FeHTTPPort: 8030}
	job := &store.MonitorJob{
		Name:      "starrocks-test",
		Collector: "test",
		Labels:    map[string]string{"domain": "dorisdb.test", "idc": "test", "version": "3.2.10", "team": "dba"},
		TargetGroups: []store.MonitorTargetGroup{
			{Endpoints: "10.0.0.1:8030", Labels: map[string]string{"domain": "dorisdb.test", "version": "3.2.10"}},
			{Endpoints: "10.0.0.1:8040,10.0.0.9:8040", Labels: map[string]string{"app": "be-StarRocks"}},
		},
	}
	labels := map[string]string{"idc": "beijing", "version": "3.3.22", "domain": "other"}
	changed := mergeMonitorJob(job, cl,
		[]string{"10.0.0.1:8030"},
		[]string{"10.0.0.1:8040", "10.0.0.2:8040"},
		[]string{"10.0.0.9:8040"}, labels)
	if !changed {
		t.Fatal("应有变更")
	}
	if len(job.TargetGroups) != 2 {
		t.Fatalf("不应新建组: %+v", job.TargetGroups)
	}
	fe, be := job.TargetGroups[0], job.TargetGroups[1]
	if fe.Labels["app"] != monitorAppFE || fe.Labels["version"] != "3.3.22" {
		t.Fatalf("FE 组按端口识别并补 app、更新 version: %+v", fe.Labels)
	}
	if be.Endpoints != "10.0.0.1:8040,10.0.0.2:8040" {
		t.Fatalf("BE 组追加新节点并移除下线节点: %q", be.Endpoints)
	}
	if job.Labels["idc"] != "test" || job.Labels["domain"] != "dorisdb.test" || job.Labels["team"] != "dba" {
		t.Fatalf("人工标签不应被覆盖: %+v", job.Labels)
	}
	if job.Labels["version"] != "3.3.22" {
		t.Fatalf("version 以平台为准: %+v", job.Labels)
	}
	if mergeMonitorJob(job, cl, []string{"10.0.0.1:8030"}, []string{"10.0.0.1:8040"}, nil, labels) {
		t.Fatal("再次同步应无变更（幂等）")
	}
}

func TestClassifyMonitorGroupPortOverLabel(t *testing.T) {
	g := store.MonitorTargetGroup{Endpoints: "10.0.0.1:8040,10.0.0.2:8040", Labels: map[string]string{"app": "fe-StarRocks"}}
	if k := classifyMonitorGroup(g, "8030", "8040"); k != "be" {
		t.Fatalf("端口一致时以端口为准: %s", k)
	}
	g = store.MonitorTargetGroup{Endpoints: "a:1,b:2", Labels: map[string]string{"app": "fe-Starrocks"}}
	if k := classifyMonitorGroup(g, "8030", "8040"); k != "fe" {
		t.Fatalf("端口不确定时看 app 标签: %s", k)
	}
}

func TestReadyNodesSkipsRemovedAndPending(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "a", Status: store.SrNodeReady},
		{ID: "b", Status: store.SrNodePending},
		{ID: "c", Status: store.SrNodeReady},
	}
	out := readyNodes(nodes, []store.SrNode{{ID: "c"}})
	if len(out) != 1 || out[0].ID != "a" {
		t.Fatalf("out=%+v", out)
	}
}
