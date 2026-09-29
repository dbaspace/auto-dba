package srcluster

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"auto-dba/internal/nodecheck"
	"auto-dba/internal/store"
)

// AddNodeCheckInput 添加节点基础校验入参。
type AddNodeCheckInput struct {
	Node           *store.SrNode
	HostInstanceID string
	AssignedTo     string // 空则取 sync_default_approver
	SkipAgentCalls bool   // true：不探活、不调 Agent 预检（建单兜底校验用）
}

// addNodeFacts 校验所需数据（与 store 解耦，便于单测）。
type addNodeFacts struct {
	Cluster      *store.SrCluster
	Nodes        []store.SrNode
	ActiveChange string
	HostInst     *store.Instance
	Agent        *store.SrAgent
	Approver     string
}

// PrecheckAddNode 提交「添加节点」工单前的基础校验。
func (e *Engine) PrecheckAddNode(ctx context.Context, clusterID string, in AddNodeCheckInput) *nodecheck.Result {
	res := nodecheck.NewResult()
	if e == nil || e.Store == nil {
		res.Add("engine", false, "engine 未初始化")
		return res.Finish()
	}
	if in.Node == nil {
		res.Add("node", false, "缺少节点信息")
		return res.Finish()
	}
	f := addNodeFacts{}
	f.Cluster, _ = e.Store.GetSrCluster(clusterID)
	if f.Cluster != nil {
		f.Nodes, _ = e.Store.ListSrNodes(f.Cluster.ID)
		if err := e.rejectIfActiveChange(f.Cluster.ID); err != nil {
			f.ActiveChange = err.Error()
		}
	}
	if id := strings.TrimSpace(in.HostInstanceID); id != "" {
		f.HostInst, _ = e.Store.GetInstance(id)
	}
	if id := strings.TrimSpace(in.Node.AgentID); id != "" {
		f.Agent, _ = e.Store.GetSrAgent(id)
	}
	f.Approver = strings.TrimSpace(in.AssignedTo)
	if f.Approver == "" {
		f.Approver, _ = e.Store.GetGlobalArg("sync_default_approver")
	}
	evalAddNode(res, f, in)

	if !in.SkipAgentCalls {
		e.addNodeAgentChecks(ctx, res, f, in.Node)
	}
	return res.Finish()
}

func evalAddNode(res *nodecheck.Result, f addNodeFacts, in AddNodeCheckInput) {
	n := in.Node
	cl := f.Cluster
	if cl == nil {
		res.Add("cluster", false, "集群不存在")
	} else if f.ActiveChange != "" {
		res.Add("cluster", false, f.ActiveChange)
	} else {
		res.Add("cluster", true, fmt.Sprintf("%s（status=%s），无进行中变更", cl.Name, cl.Status))
	}

	host := strings.TrimSpace(n.Host)
	hostInstID := strings.TrimSpace(in.HostInstanceID)
	addrs := []string{host}
	switch {
	case hostInstID == "":
		res.Add("host_instance", false, "请选择已登记的主机实例")
	case f.HostInst == nil:
		res.Add("host_instance", false, "主机实例不存在")
	case !strings.EqualFold(strings.TrimSpace(f.HostInst.DBType), "host"):
		res.Add("host_instance", false, fmt.Sprintf("实例 %s 不是主机类型（db_type=%s）", f.HostInst.Name, f.HostInst.DBType))
	case nodecheck.PrimaryAddr(f.HostInst) == "":
		res.Add("host_instance", false, fmt.Sprintf("主机 %s 未填写 Host/域名", f.HostInst.Name))
	default:
		addrs = nodecheck.HostAddrs(f.HostInst)
		if host != "" && !nodecheck.AgentMatchesHost("http://"+host, "", "", addrs...) {
			res.Add("host_instance", false, fmt.Sprintf("Host %s 与主机实例 %s 地址不一致", host, f.HostInst.Name))
		} else {
			res.Add("host_instance", true, fmt.Sprintf("%s（%s）", f.HostInst.Name, strings.Join(addrs, ", ")))
		}
	}

	switch {
	case f.Agent == nil:
		res.Add("agent_match", false, "Node Agent 不存在，请先登记或 SSH 下发")
	case !nodecheck.AgentMatchesHost(f.Agent.BaseURL, f.Agent.HostInstID, hostInstID, addrs...):
		res.Add("agent_match", false, nodecheck.MismatchMessage(f.Agent.BaseURL, host))
	default:
		res.Add("agent_match", true, fmt.Sprintf("%s 与主机 %s 一致", f.Agent.BaseURL, host))
	}

	dup := ""
	feCount := 0
	hasPrimary := false
	for _, x := range f.Nodes {
		if x.Component == "fe" {
			feCount++
			if x.IsPrimaryFE {
				hasPrimary = true
			}
		}
		if x.Component == n.Component && strings.EqualFold(strings.TrimSpace(x.Host), host) {
			dup = fmt.Sprintf("集群中已存在 %s@%s（状态 %s）", x.Component, x.Host, x.Status)
		}
	}
	if dup != "" {
		res.Add("duplicate", false, dup)
	} else {
		res.Add("duplicate", true, fmt.Sprintf("集群中无 %s@%s", n.Component, host))
	}

	if n.Component == "fe" {
		switch {
		case n.IsPrimaryFE && cl != nil && cl.Status == store.SrClusterReady:
			res.Add("role_rule", false, "集群已就绪，扩容 FE 不能勾选「首 FE」")
		case n.IsPrimaryFE && hasPrimary:
			res.Add("role_rule", false, "集群已有首 FE，不能再勾选「首 FE」")
		default:
			res.Add("role_rule", true, fmt.Sprintf("FE 子角色 %s", n.FeRole))
			if after := feCount + 1; after%2 == 0 {
				res.Warn("fe_count", fmt.Sprintf("扩容后 FE 共 %d 个（偶数），扩容编排要求 FE 总数为奇数，需再添加一个 FE", after))
			}
		}
	} else {
		dr := strings.TrimSpace(n.DataRoot)
		if !path.IsAbs(dr) || path.Clean(dr) == "/" {
			res.Add("role_rule", false, fmt.Sprintf("BE 数据目录 %q 须为绝对路径且不能是 /", dr))
		} else {
			res.Add("role_rule", true, "BE 数据目录 "+dr)
		}
	}

	if strings.TrimSpace(f.Approver) == "" {
		res.Add("approver", false, "审批人为空，且全局参数 sync_default_approver 未配置")
	} else {
		res.Add("approver", true, f.Approver)
	}
}

func (e *Engine) addNodeAgentChecks(ctx context.Context, res *nodecheck.Result, f addNodeFacts, n *store.SrNode) {
	isBE := n.Component == "be"
	dataRootUnverified := func(reason string) {
		if isBE {
			mergeDataRootIntoRoleRule(res, nil, n.DataRoot, reason)
		}
	}
	if f.Agent == nil || res.Failed("agent_match") != nil {
		res.Add("agent_online", false, "Agent 未通过地址校验，跳过探活")
		dataRootUnverified("Agent 未通过地址校验，无法校验数据目录")
		return
	}
	auth, err := e.agentAuth(f.Agent.ID)
	if err != nil {
		res.Add("agent_online", false, err.Error())
		dataRootUnverified("Agent 不可用，无法校验数据目录")
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = e.Agent.Probe(pctx, auth)
	cancel()
	if err != nil {
		res.Add("agent_online", false, fmt.Sprintf("%s 探活失败: %v", auth.BaseURL, err))
		res.Add("agent_precheck", false, "Agent 离线，跳过环境预检")
		dataRootUnverified("Agent 离线，无法校验数据目录挂载")
		return
	}
	res.Add("agent_online", true, auth.BaseURL+" 在线")

	req := PrecheckReq{Role: n.Component, HTTPPort: n.HTTPPort, EditPort: n.EditPort, HBPort: n.HeartbeatPort}
	if isBE {
		req.DataRoot = n.DataRoot
	}
	if cl := f.Cluster; cl != nil {
		req.QueryPort = cl.FeQueryPort
		if req.HTTPPort <= 0 {
			req.HTTPPort = cl.FeHTTPPort
		}
		if req.EditPort <= 0 {
			req.EditPort = cl.FeEditPort
		}
		if req.HBPort <= 0 {
			req.HBPort = cl.BeHeartbeatPort
		}
	}
	pctx, cancel = context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := e.Agent.Precheck(pctx, auth, req)
	if err != nil {
		res.Add("agent_precheck", false, "Agent 预检请求失败: "+err.Error())
		dataRootUnverified("Agent 预检请求失败，无法校验数据目录")
		return
	}
	applyAgentPrecheck(res, resp, n)
}

// applyAgentPrecheck 合并 Agent 预检：data_root 并入 role_rule，warn 项仅提醒。
func applyAgentPrecheck(res *nodecheck.Result, resp *PrecheckResp, n *store.SrNode) {
	hardFail := false
	for _, it := range resp.Items {
		switch {
		case it.Name == "data_root":
			continue
		case it.Level == "warn":
			res.Warn("agent:"+it.Name, it.Message)
		default:
			res.Add("agent:"+it.Name, it.OK, it.Message)
			if !it.OK {
				hardFail = true
			}
		}
	}
	if n.Component == "be" {
		mergeDataRootIntoRoleRule(res, resp.Items, n.DataRoot, "")
	}
	if !resp.OK && len(resp.Items) == 0 {
		msg := strings.TrimSpace(resp.Msg)
		if msg == "" {
			msg = "Agent 预检未通过"
		}
		res.Add("agent_precheck", false, msg)
	} else if len(resp.Items) == 0 && !hardFail {
		res.Add("agent_precheck", true, "Agent 预检通过")
	}
}

// mergeDataRootIntoRoleRule 数据目录结论以 role_rule 为准：Agent 的 data_root 判定覆盖 role_rule；
// reason 非空表示无法调用 Agent。静态规则已失败时保持原结论。
func mergeDataRootIntoRoleRule(res *nodecheck.Result, items []PrecheckItem, dataRoot, reason string) {
	if rr := res.Get("role_rule"); rr != nil && !rr.OK {
		return
	}
	if reason != "" {
		res.Set("role_rule", false, fmt.Sprintf("BE 数据目录 %s：%s", dataRoot, reason))
		return
	}
	for _, it := range items {
		if it.Name != "data_root" {
			continue
		}
		res.Set("role_rule", it.OK, "BE "+it.Message)
		return
	}
	res.Set("role_rule", false, fmt.Sprintf("BE 数据目录 %s：%s", dataRoot, OldAgentDataRootMsg))
}

// OldAgentDataRootMsg 旧版 Node Agent 不返回 data_root 校验项。
const OldAgentDataRootMsg = "Node Agent 版本过旧，不支持数据目录校验，请重新 SSH 下发 Agent"
