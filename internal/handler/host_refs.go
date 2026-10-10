package handler

import (
	"strings"

	"auto-dba/internal/nodecheck"
	"auto-dba/internal/store"
)

// hostRef 主机 / Agent 仍被引用的对象（删除前拦截用）。
type hostRef struct {
	Kind   string `json:"kind"` // sr_agent | db_agent | platform_agent | sr_node | db_node
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

var hostRefKindLabel = map[string]string{
	"sr_agent":       "Node Agent",
	"db_agent":       "DB Agent",
	"platform_agent": "Platform Agent",
	"sr_node":        "StarRocks 节点",
	"db_node":        "DB 节点",
}

type agentLite struct{ ID, Name, BaseURL, HostInstID string }

func matchHostAgents(kind string, agents []agentLite, hostID string, addrs []string) []hostRef {
	var out []hostRef
	for _, a := range agents {
		if !nodecheck.AgentMatchesHost(a.BaseURL, a.HostInstID, hostID, addrs...) {
			continue
		}
		name := strings.TrimSpace(a.Name)
		if name == "" {
			name = a.ID
		}
		out = append(out, hostRef{Kind: kind, ID: a.ID, Name: name, Detail: strings.TrimSpace(a.BaseURL)})
	}
	return out
}

func formatHostRefs(refs []hostRef) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		s := r.Name
		if label := hostRefKindLabel[r.Kind]; label != "" {
			s = label + " " + r.Name
		}
		if r.Detail != "" {
			s += "（" + r.Detail + "）"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "、")
}

// agentAddrs 手工登记 Agent 时匹配节点用的地址（登记主机的地址加 base_url 主机名）。
func agentAddrs(st *store.DB, hostInstID, baseURL string) []string {
	var inst *store.Instance
	if id := strings.TrimSpace(hostInstID); id != "" {
		inst, _ = st.GetInstance(id)
	}
	return nodecheck.AgentAddrs(inst, baseURL)
}

type clusterNames struct {
	st *store.DB
	sr map[string]string
	db map[string]string
}

func newClusterNames(st *store.DB) *clusterNames {
	return &clusterNames{st: st, sr: map[string]string{}, db: map[string]string{}}
}

func (c *clusterNames) srName(id string) string {
	if v, ok := c.sr[id]; ok {
		return v
	}
	v := id
	if cl, _ := c.st.GetSrCluster(id); cl != nil && cl.Name != "" {
		v = cl.Name
	}
	c.sr[id] = v
	return v
}

func (c *clusterNames) dbName(id string) string {
	if v, ok := c.db[id]; ok {
		return v
	}
	v := id
	if cl, _ := c.st.GetDbDeployCluster(id); cl != nil && cl.Name != "" {
		v = cl.Name
	}
	c.db[id] = v
	return v
}

func srNodeRefs(names *clusterNames, nodes []store.SrNode) []hostRef {
	out := make([]hostRef, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, hostRef{Kind: "sr_node", ID: n.ID, Name: n.Component + "@" + n.Host, Detail: "集群 " + names.srName(n.ClusterID)})
	}
	return out
}

func dbNodeRefs(names *clusterNames, nodes []store.DbDeployNode) []hostRef {
	out := make([]hostRef, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, hostRef{Kind: "db_node", ID: n.ID, Name: n.Role + "@" + n.Host, Detail: "集群 " + names.dbName(n.ClusterID)})
	}
	return out
}

// collectHostRefs 主机上登记的 Agent（Node / DB / Platform）与集群节点。
func collectHostRefs(st *store.DB, inst *store.Instance) ([]hostRef, error) {
	addrs := nodecheck.HostAddrs(inst)
	var refs []hostRef

	srAgents, err := st.ListSrAgentsInTenant(inst.TenantID)
	if err != nil {
		return nil, err
	}
	lites := make([]agentLite, 0, len(srAgents))
	for _, a := range srAgents {
		lites = append(lites, agentLite{a.ID, a.Name, a.BaseURL, a.HostInstID})
	}
	refs = append(refs, matchHostAgents("sr_agent", lites, inst.ID, addrs)...)

	dbAgents, err := st.ListDbDeployAgentsInTenant(inst.TenantID)
	if err != nil {
		return nil, err
	}
	lites = lites[:0]
	for _, a := range dbAgents {
		lites = append(lites, agentLite{a.ID, a.Name, a.BaseURL, a.HostInstID})
	}
	refs = append(refs, matchHostAgents("db_agent", lites, inst.ID, addrs)...)

	pAgents, err := st.ListBackupAgentsInTenant(inst.TenantID)
	if err != nil {
		return nil, err
	}
	lites = lites[:0]
	for _, a := range pAgents {
		lites = append(lites, agentLite{a.ID, a.Name, a.BaseURL, store.ParseBackupAgentHostTag(a.Tags)})
	}
	refs = append(refs, matchHostAgents("platform_agent", lites, inst.ID, addrs)...)

	names := newClusterNames(st)
	srNodes, err := st.ListSrNodesByHostRef(inst.ID, addrs)
	if err != nil {
		return nil, err
	}
	srNodes = filterNodesByClusterTenant(st, "tbl_sr_clusters", inst.TenantID, srNodes, func(n *store.SrNode) string { return n.ClusterID })
	refs = append(refs, srNodeRefs(names, srNodes)...)
	dbNodes, err := st.ListDbDeployNodesByHostRef(inst.ID, addrs)
	if err != nil {
		return nil, err
	}
	dbNodes = filterNodesByClusterTenant(st, "tbl_db_deploy_clusters", inst.TenantID, dbNodes, func(n *store.DbDeployNode) string { return n.ClusterID })
	refs = append(refs, dbNodeRefs(names, dbNodes)...)
	return refs, nil
}

// filterNodesByClusterTenant 仅保留所属集群在 tenantID 下的节点。
func filterNodesByClusterTenant[T any](st *store.DB, table, tenantID string, nodes []T, clusterOf func(*T) string) []T {
	if strings.TrimSpace(tenantID) == "" {
		return nodes
	}
	seen := map[string]bool{}
	out := nodes[:0]
	for i := range nodes {
		cid := clusterOf(&nodes[i])
		ok, cached := seen[cid]
		if !cached {
			ok, _ = st.RowVisibleInTenant(table, cid, tenantID)
			seen[cid] = ok
		}
		if ok {
			out = append(out, nodes[i])
		}
	}
	return out
}
