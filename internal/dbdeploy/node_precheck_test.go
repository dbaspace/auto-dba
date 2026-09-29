package dbdeploy

import (
	"strings"
	"testing"

	"auto-dba/internal/nodecheck"
	"auto-dba/internal/store"
)

func baseDbAddNodeFacts() addNodeFacts {
	return addNodeFacts{
		Cluster:  &store.DbDeployCluster{ID: "c1", Name: "redis-prod", Engine: "redis", DeployKind: "self", Status: "ready"},
		Nodes:    []store.DbDeployNode{{ID: "n1", ClusterID: "c1", Role: "primary", Host: "10.0.0.1", Port: 6379, Status: "ready"}},
		HostInst: &store.Instance{ID: "h17", Name: "node17", DBType: "host", Host: "10.100.112.17"},
		Agent:    &store.DbDeployAgent{ID: "a17", BaseURL: "http://10.100.112.17:8092"},
		Approver: "admin",
	}
}

func runDbEval(f addNodeFacts, n *store.DbDeployNode) *nodecheck.Result {
	res := nodecheck.NewResult()
	evalAddNode(res, f, AddNodeCheckInput{Node: n, HostInstanceID: "h17"})
	return res.Finish()
}

func TestDbEvalAddNodeOK(t *testing.T) {
	res := runDbEval(baseDbAddNodeFacts(), &store.DbDeployNode{Role: "replica", Host: "10.100.112.17", AgentID: "a17"})
	if !res.OK {
		t.Fatalf("应通过: %+v", res.Items)
	}
}

func TestDbEvalAddNodeAgentMismatch(t *testing.T) {
	f := baseDbAddNodeFacts()
	f.Agent = &store.DbDeployAgent{ID: "a18", BaseURL: "http://10.100.112.18:8092"}
	res := runDbEval(f, &store.DbDeployNode{Role: "replica", Host: "10.100.112.17", AgentID: "a18"})
	if bad := res.Failed("agent_match"); bad == nil || !strings.Contains(bad.Message, "10.100.112.18") {
		t.Fatalf("应报 Agent 地址不一致: %+v", res.Items)
	}
}

func TestDbEvalAddNodeDuplicateAndBadRole(t *testing.T) {
	f := baseDbAddNodeFacts()
	f.Nodes = append(f.Nodes, store.DbDeployNode{ClusterID: "c1", Role: "replica", Host: "10.100.112.17"})
	res := runDbEval(f, &store.DbDeployNode{Role: "replica", Host: "10.100.112.17", AgentID: "a17"})
	if res.Failed("duplicate") == nil {
		t.Fatalf("重复节点应失败: %+v", res.Items)
	}
	res = runDbEval(baseDbAddNodeFacts(), &store.DbDeployNode{Role: "tikv", Host: "10.100.112.17", AgentID: "a17"})
	if res.Failed("role_rule") == nil {
		t.Fatalf("redis 不允许 tikv 角色: %+v", res.Items)
	}
}

func TestDbEvalAddNodePortConflict(t *testing.T) {
	f := baseDbAddNodeFacts()
	f.HostNodes = []store.DbDeployNode{{ID: "x1", ClusterID: "c2", Role: "single", Host: "10.100.112.17", Port: 6379}}
	res := runDbEval(f, &store.DbDeployNode{Role: "replica", Host: "10.100.112.17", AgentID: "a17"})
	if res.Failed("role_rule") == nil {
		t.Fatalf("同主机 6379 已占用应失败: %+v", res.Items)
	}
	res = runDbEval(f, &store.DbDeployNode{Role: "sentinel", Host: "10.100.112.17", AgentID: "a17"})
	if !res.OK {
		t.Fatalf("sentinel 默认 26379 不冲突: %+v", res.Items)
	}
}

func TestDbEvalAddNodeCloudCluster(t *testing.T) {
	f := baseDbAddNodeFacts()
	f.Cluster.DeployKind = "cloud"
	res := runDbEval(f, &store.DbDeployNode{Role: "replica", Host: "10.100.112.17", AgentID: "a17"})
	if res.Failed("cluster") == nil {
		t.Fatalf("云托管集群应拒绝: %+v", res.Items)
	}
}
