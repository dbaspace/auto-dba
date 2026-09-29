package dbdeploy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/nodecheck"
	"auto-dba/internal/store"
)

// AddNodeCheckInput 添加节点基础校验入参。
type AddNodeCheckInput struct {
	Node           *store.DbDeployNode
	HostInstanceID string
	AssignedTo     string // 空则取 sync_default_approver
	SkipAgentCalls bool   // true：不探活、不调 Agent 预检（建单兜底校验用）
}

// addNodeFacts 校验所需数据（与 store 解耦，便于单测）。
type addNodeFacts struct {
	Cluster      *store.DbDeployCluster
	Nodes        []store.DbDeployNode // 本集群节点
	HostNodes    []store.DbDeployNode // 所有集群中同主机节点
	ActiveChange string
	HostInst     *store.Instance
	Agent        *store.DbDeployAgent
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
	f.Cluster, _ = e.Store.GetDbDeployCluster(clusterID)
	if f.Cluster != nil {
		f.Nodes, _ = e.Store.ListDbDeployNodes(f.Cluster.ID)
		if err := e.rejectIfActiveChange(f.Cluster.ID); err != nil {
			f.ActiveChange = err.Error()
		}
	}
	if h := strings.TrimSpace(in.Node.Host); h != "" {
		f.HostNodes, _ = e.Store.ListDbDeployNodesByHost(h)
		engines := map[string]string{}
		for i := range f.HostNodes {
			x := &f.HostNodes[i]
			if x.Port > 0 {
				continue
			}
			eng, ok := engines[x.ClusterID]
			if !ok {
				if c, _ := e.Store.GetDbDeployCluster(x.ClusterID); c != nil {
					eng = c.Engine
				}
				engines[x.ClusterID] = eng
			}
			x.Port = effectivePort(eng, *x)
		}
	}
	if id := strings.TrimSpace(in.HostInstanceID); id != "" {
		f.HostInst, _ = e.Store.GetInstance(id)
	}
	if id := strings.TrimSpace(in.Node.AgentID); id != "" {
		f.Agent, _ = e.Store.GetDbDeployAgent(id)
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

// effectivePort 节点实际端口（未填按角色/引擎默认，与 nodePort 一致）。
func effectivePort(engine string, n store.DbDeployNode) int {
	return nodePort(&store.DbDeployCluster{Engine: engine}, &n)
}

func evalAddNode(res *nodecheck.Result, f addNodeFacts, in AddNodeCheckInput) {
	n := in.Node
	cl := f.Cluster
	engine := ""
	if cl == nil {
		res.Add("cluster", false, "集群不存在")
	} else {
		engine = cl.Engine
		switch {
		case f.ActiveChange != "":
			res.Add("cluster", false, f.ActiveChange)
		case strings.EqualFold(cl.DeployKind, "cloud"):
			res.Add("cluster", false, "云托管集群不支持添加自建节点")
		default:
			res.Add("cluster", true, fmt.Sprintf("%s（status=%s），无进行中变更", cl.Name, cl.Status))
		}
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
		res.Add("agent_match", false, "DB Agent 不存在，请先登记或 SSH 下发")
	case !nodecheck.AgentMatchesHost(f.Agent.BaseURL, f.Agent.HostInstID, hostInstID, addrs...):
		res.Add("agent_match", false, nodecheck.MismatchMessage(f.Agent.BaseURL, host))
	default:
		res.Add("agent_match", true, fmt.Sprintf("%s 与主机 %s 一致", f.Agent.BaseURL, host))
	}

	dup := ""
	for _, x := range f.Nodes {
		if x.Role == n.Role && strings.EqualFold(strings.TrimSpace(x.Host), host) {
			dup = fmt.Sprintf("集群中已存在 %s@%s（状态 %s）", x.Role, x.Host, x.Status)
		}
	}
	if dup != "" {
		res.Add("duplicate", false, dup)
	} else {
		res.Add("duplicate", true, fmt.Sprintf("集群中无 %s@%s", n.Role, host))
	}

	var roleErr string
	allowed := []string{}
	if d := GetEngine(engine); d != nil {
		allowed = d.Roles
	}
	port := effectivePort(engine, *n)
	switch {
	case len(allowed) > 0 && !containsFold(allowed, n.Role):
		roleErr = fmt.Sprintf("角色 %s 不适用于 %s（可选 %s）", n.Role, engine, strings.Join(allowed, "/"))
	case port < 1 || port > 65535:
		roleErr = fmt.Sprintf("端口 %d 不合法（1-65535）", port)
	default:
		for _, x := range f.HostNodes {
			if x.ID != "" && x.ID == n.ID {
				continue
			}
			if x.Port > 0 && x.Port == port {
				roleErr = fmt.Sprintf("主机 %s 端口 %d 已被节点 %s（集群 %s）占用", host, port, x.Role, x.ClusterID)
				break
			}
		}
	}
	if roleErr != "" {
		res.Add("role_rule", false, roleErr)
	} else {
		res.Add("role_rule", true, fmt.Sprintf("角色 %s，端口 %d", n.Role, port))
	}

	if strings.TrimSpace(f.Approver) == "" {
		res.Add("approver", false, "审批人为空，且全局参数 sync_default_approver 未配置")
	} else {
		res.Add("approver", true, f.Approver)
	}
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

func (e *Engine) addNodeAgentChecks(ctx context.Context, res *nodecheck.Result, f addNodeFacts, n *store.DbDeployNode) {
	if f.Agent == nil || res.Failed("agent_match") != nil {
		res.Add("agent_online", false, "Agent 未通过地址校验，跳过探活")
		return
	}
	auth, err := e.agentAuthWithPassword(f.Agent.ID)
	if err != nil {
		res.Add("agent_online", false, err.Error())
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = e.Agent.Probe(pctx, auth)
	cancel()
	if err != nil {
		res.Add("agent_online", false, fmt.Sprintf("%s 探活失败: %v", auth.BaseURL, err))
		res.Add("agent_precheck", false, "Agent 离线，跳过环境预检")
		return
	}
	res.Add("agent_online", true, auth.BaseURL+" 在线")

	engine := ""
	if f.Cluster != nil {
		engine = f.Cluster.Engine
	}
	pctx, cancel = context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := e.Agent.Precheck(pctx, auth, map[string]any{
		"engine": engine, "role": n.Role, "port": effectivePort(engine, *n),
	})
	if err != nil {
		res.Add("agent_precheck", false, "Agent 预检请求失败: "+err.Error())
		return
	}
	for _, it := range resp.Items {
		name, _ := it["name"].(string)
		ok, _ := it["ok"].(bool)
		msg, _ := it["message"].(string)
		if name == "" {
			continue
		}
		res.Add("agent:"+name, ok, msg)
	}
	if !resp.OK {
		msg := strings.TrimSpace(resp.Msg)
		if msg == "" {
			msg = "Agent 预检未通过"
		}
		res.Add("agent_precheck", false, msg)
	} else {
		res.Add("agent_precheck", true, "Agent 预检通过")
	}
}
