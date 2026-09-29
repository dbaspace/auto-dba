package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/nodecheck"
	"auto-dba/internal/store"
)

func baseAddNodeFacts() addNodeFacts {
	return addNodeFacts{
		Cluster:  &store.SrCluster{ID: "c1", Name: "sr-prod", Status: store.SrClusterReady},
		Nodes:    []store.SrNode{{Component: "fe", Host: "10.0.0.1", IsPrimaryFE: true, Status: "ready"}},
		HostInst: &store.Instance{ID: "h17", Name: "node17", DBType: "host", Host: "10.100.112.17"},
		Agent:    &store.SrAgent{ID: "a17", BaseURL: "http://10.100.112.17:8091"},
		Approver: "admin",
	}
}

func runEval(f addNodeFacts, n *store.SrNode) *nodecheck.Result {
	res := nodecheck.NewResult()
	evalAddNode(res, f, AddNodeCheckInput{Node: n, HostInstanceID: "h17"})
	return res.Finish()
}

func TestEvalAddNodeOK(t *testing.T) {
	res := runEval(baseAddNodeFacts(), &store.SrNode{Component: "be", Host: "10.100.112.17", AgentID: "a17", DataRoot: "/data"})
	if !res.OK {
		t.Fatalf("应通过: %+v", res.Items)
	}
}

func TestEvalAddNodeAgentMismatch(t *testing.T) {
	f := baseAddNodeFacts()
	f.Agent = &store.SrAgent{ID: "a18", BaseURL: "http://10.100.112.18:8091", HostInstID: "h18"}
	res := runEval(f, &store.SrNode{Component: "be", Host: "10.100.112.17", AgentID: "a18", DataRoot: "/data"})
	bad := res.Failed("agent_match")
	if bad == nil || !strings.Contains(bad.Message, "10.100.112.18") {
		t.Fatalf("应报 Agent 地址不一致: %+v", res.Items)
	}
}

func TestEvalAddNodeHostNotInInstance(t *testing.T) {
	res := runEval(baseAddNodeFacts(), &store.SrNode{Component: "be", Host: "10.100.112.99", AgentID: "a17", DataRoot: "/data"})
	if res.Failed("host_instance") == nil {
		t.Fatalf("Host 与主机实例不一致应失败: %+v", res.Items)
	}
}

func TestEvalAddNodeDuplicate(t *testing.T) {
	f := baseAddNodeFacts()
	f.Nodes = append(f.Nodes, store.SrNode{Component: "be", Host: "10.100.112.17", Status: "pending"})
	res := runEval(f, &store.SrNode{Component: "be", Host: "10.100.112.17", AgentID: "a17", DataRoot: "/data"})
	if res.Failed("duplicate") == nil {
		t.Fatalf("重复节点应失败: %+v", res.Items)
	}
}

func TestEvalAddNodePrimaryFEOnReadyCluster(t *testing.T) {
	res := runEval(baseAddNodeFacts(), &store.SrNode{Component: "fe", FeRole: "follower", Host: "10.100.112.17", AgentID: "a17", IsPrimaryFE: true})
	if res.Failed("role_rule") == nil {
		t.Fatalf("已就绪集群勾首 FE 应失败: %+v", res.Items)
	}
}

func TestEvalAddNodeEvenFEWarnOnly(t *testing.T) {
	res := runEval(baseAddNodeFacts(), &store.SrNode{Component: "fe", FeRole: "follower", Host: "10.100.112.17", AgentID: "a17"})
	if !res.OK {
		t.Fatalf("FE 偶数只提醒: %+v", res.Items)
	}
	warned := false
	for _, it := range res.Items {
		if it.Name == "fe_count" && it.Level == "warn" {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("应有 fe_count 提醒: %+v", res.Items)
	}
}

func beNode(dataRoot string) *store.SrNode {
	return &store.SrNode{Component: "be", Host: "10.100.112.17", AgentID: "a17", DataRoot: dataRoot}
}

func TestApplyAgentPrecheckDataRootRules(t *testing.T) {
	cases := []struct {
		name   string
		item   PrecheckItem
		wantOK bool
		want   string
	}{
		{"规则1 无空闲盘", PrecheckItem{Name: "data_root", OK: false, Message: "数据目录 /data3 不存在，且没有未使用的磁盘（lsblk -f），请先加盘"}, false, "没有未使用的磁盘"},
		{"规则1 有空闲盘", PrecheckItem{Name: "data_root", OK: true, Message: "数据目录 /data3 不存在；检测到未使用磁盘 /dev/vdc（500G），审批执行时将格式化为 xfs 并挂载到 /data3"}, true, "vdc"},
		{"规则2 系统盘", PrecheckItem{Name: "data_root", OK: false, Message: "数据目录 /data 位于系统盘 vda（/dev/vda2 挂载于 /），BE 数据目录不能使用系统盘"}, false, "系统盘"},
		{"规则3 直接使用", PrecheckItem{Name: "data_root", OK: true, Message: "数据目录 /data1 位于 vdb（/dev/vdb xfs，挂载于 /data1），可直接使用"}, true, "vdb"},
	}
	for _, c := range cases {
		n := beNode("/data3")
		res := runEval(baseAddNodeFacts(), n)
		resp := &PrecheckResp{OK: c.item.OK, Items: []PrecheckItem{
			{Name: "heartbeat_port", OK: true, Message: "port 9050 free"},
			c.item,
			{Name: "extra_disk", OK: true, Level: "warn", Message: "未使用磁盘: 无；系统盘: vda；已挂载: vdb /data1"},
		}}
		applyAgentPrecheck(res, resp, n)
		res.Finish()
		rr := res.Get("role_rule")
		if rr == nil || rr.OK != c.wantOK || !strings.Contains(rr.Message, c.want) || res.OK != c.wantOK {
			t.Fatalf("%s: role_rule=%+v ok=%v", c.name, rr, res.OK)
		}
		if res.Get("agent:data_root") != nil {
			t.Fatalf("%s: data_root 不应单独显示", c.name)
		}
		if ed := res.Get("agent:extra_disk"); ed == nil || ed.Level != "warn" || !ed.OK {
			t.Fatalf("%s: extra_disk 应仅提醒: %+v", c.name, ed)
		}
	}
}

func TestApplyAgentPrecheckOldAgent(t *testing.T) {
	n := beNode("/data2")
	res := runEval(baseAddNodeFacts(), n)
	applyAgentPrecheck(res, &PrecheckResp{OK: true, Items: []PrecheckItem{
		{Name: "extra_disk", OK: true, Message: "vda disk\nvdb disk /data1"},
	}}, n)
	res.Finish()
	if rr := res.Get("role_rule"); rr == nil || rr.OK || !strings.Contains(rr.Message, "版本过旧") {
		t.Fatalf("旧 Agent 应判失败: %+v", rr)
	}
}

func TestMergeDataRootAgentOffline(t *testing.T) {
	n := beNode("/data2")
	res := runEval(baseAddNodeFacts(), n)
	mergeDataRootIntoRoleRule(res, nil, n.DataRoot, "Agent 离线，无法校验数据目录挂载")
	res.Finish()
	if rr := res.Get("role_rule"); rr == nil || rr.OK || res.OK {
		t.Fatalf("Agent 离线应判失败: %+v", rr)
	}
}

func TestEvalAddNodeBadDataRootAndNoApprover(t *testing.T) {
	f := baseAddNodeFacts()
	f.Approver = ""
	res := runEval(f, &store.SrNode{Component: "be", Host: "10.100.112.17", AgentID: "a17", DataRoot: "data"})
	if res.Failed("role_rule") == nil || res.Failed("approver") == nil {
		t.Fatalf("相对路径和空审批人应失败: %+v", res.Items)
	}
}
