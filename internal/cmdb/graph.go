package cmdb

import (
	"fmt"
	"net"
	"strings"

	"auto-dba/internal/store"
)

// graphExpandCtx 拓扑展开上下文：用于收敛「主机 → 同机全部实例」等噪声边。
type graphExpandCtx struct {
	RootType    string
	RootID      string
	SrClusterID string // 非空时 TypeHost 只展开该 SR 集群相关实例
}

// BuildGraph 推导关系 + 显式关系，depth=1|2|3。
func (s *Service) BuildGraph(ciType, ciID string, depth int) (*Graph, error) {
	return s.BuildGraphInTenant(ciType, ciID, "", depth)
}

func (s *Service) BuildGraphInTenant(ciType, ciID, tenantID string, depth int) (*Graph, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	root, err := s.GetCIInTenant(ciType, ciID, tenantID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("配置项不存在")
	}
	nodes := map[string]CI{nodeKey(root.Type, root.ID): *root}
	var edges []Edge
	seenEdge := map[string]bool{}

	addNode := func(c CI) {
		if c.ID == "" {
			return
		}
		nodes[nodeKey(c.Type, c.ID)] = c
	}
	addEdge := func(e Edge) {
		k := e.SrcType + "|" + e.SrcID + "|" + e.RelType + "|" + e.DstType + "|" + e.DstID
		if seenEdge[k] {
			return
		}
		seenEdge[k] = true
		edges = append(edges, e)
	}

	ctx := s.resolveExpandCtx(root.Type, root.ID)
	if err := s.collectNeighbors(root.Type, root.ID, depth, ctx, addNode, addEdge); err != nil {
		return nil, err
	}

	outNodes := make([]CI, 0, len(nodes))
	for _, n := range nodes {
		outNodes = append(outNodes, n)
	}
	return &Graph{Nodes: outNodes, Edges: edges}, nil
}

func nodeKey(t, id string) string { return t + "|" + id }

func (s *Service) resolveExpandCtx(rootType, rootID string) graphExpandCtx {
	ctx := graphExpandCtx{RootType: rootType, RootID: rootID}
	// 从主机本身出发：保留同机全量实例视图
	if rootType == TypeHost {
		return ctx
	}
	if rootType == TypeSrCluster {
		ctx.SrClusterID = strings.TrimSpace(rootID)
		return ctx
	}
	if rootType == TypeInstance {
		it, _ := s.Store.GetInstance(rootID)
		if it != nil {
			if cid := srClusterIDFromTags(it.Tags); cid != "" {
				ctx.SrClusterID = cid
			}
		}
	}
	return ctx
}

func (s *Service) collectNeighbors(ciType, ciID string, depth int, ctx graphExpandCtx, addNode func(CI), addEdge func(Edge)) error {
	seen := map[string]bool{}
	queue := []struct {
		t, id string
		d     int
	}{{ciType, ciID, 0}}

	processEdges := func(srcType, srcID string) ([]Edge, error) {
		var out []Edge
		d1, err := s.derivedEdges(srcType, srcID, ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, d1...)
		m1, _ := s.Store.ListCmdbRelations(srcType, srcID, "", "")
		for _, r := range m1 {
			out = append(out, Edge{SrcType: r.SrcType, SrcID: r.SrcID, RelType: r.RelType, DstType: r.DstType, DstID: r.DstID, Source: r.Source, Remark: r.Remark})
		}
		m2, _ := s.Store.ListCmdbRelations("", "", srcType, srcID)
		for _, r := range m2 {
			out = append(out, Edge{SrcType: r.SrcType, SrcID: r.SrcID, RelType: r.RelType, DstType: r.DstType, DstID: r.DstID, Source: r.Source, Remark: r.Remark})
		}
		return out, nil
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		k := nodeKey(cur.t, cur.id)
		if seen[k] {
			continue
		}
		seen[k] = true
		if cur.d >= depth {
			continue
		}
		eds, err := processEdges(cur.t, cur.id)
		if err != nil {
			return err
		}
		for _, e := range eds {
			addEdge(e)
			for _, peer := range []struct{ t, id string }{{e.SrcType, e.SrcID}, {e.DstType, e.DstID}} {
				if peer.t == cur.t && peer.id == cur.id {
					continue
				}
				ci, _ := s.GetCI(peer.t, peer.id)
				if ci != nil {
					addNode(*ci)
					queue = append(queue, struct {
						t, id string
						d     int
					}{peer.t, peer.id, cur.d + 1})
				} else {
					addNode(CI{ID: peer.id, Type: peer.t, Name: peer.id})
				}
			}
		}
	}
	return nil
}

func (s *Service) derivedEdges(ciType, ciID string, ctx graphExpandCtx) ([]Edge, error) {
	var out []Edge
	switch ciType {
	case TypeDatabase:
		db, err := s.Store.GetDatabase(ciID)
		if err != nil || db == nil {
			return out, err
		}
		out = append(out, Edge{SrcType: TypeDatabase, SrcID: db.ID, RelType: "hosted_on", DstType: TypeInstance, DstID: db.InstanceID, Source: "derived"})
		accs, _ := s.Store.ListAccounts("", "", db.InstanceID, "")
		for _, a := range accs {
			if a.DatabaseID == db.ID || (a.DatabaseID == "" && a.DBName == db.DBName) {
				out = append(out, Edge{SrcType: TypeDatabase, SrcID: db.ID, RelType: "has_account", DstType: TypeDBAccount, DstID: a.ID, Source: "derived"})
			}
		}
	case TypeInstance:
		it, err := s.Store.GetInstance(ciID)
		if err != nil || it == nil {
			return out, err
		}
		if it.EnvID != "" {
			out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "in_env", DstType: TypeEnvironment, DstID: it.EnvID, Source: "derived"})
		}
		if it.RegionID != "" {
			out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "in_region", DstType: TypeRegion, DstID: it.RegionID, Source: "derived"})
		}
		host := strings.TrimSpace(it.Host)
		if hh, _, err := net.SplitHostPort(host); err == nil {
			host = hh
		}
		if host != "" {
			out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "runs_on", DstType: TypeHost, DstID: "host:" + host, Source: "derived"})
		}
		dbs, _ := s.Store.ListDatabases(it.ID, "")
		for _, db := range dbs {
			out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "contains", DstType: TypeDatabase, DstID: db.ID, Source: "derived"})
			out = append(out, Edge{SrcType: TypeDatabase, SrcID: db.ID, RelType: "hosted_on", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
		}
		accs, _ := s.Store.ListAccounts("", "", it.ID, "")
		for _, a := range accs {
			out = append(out, Edge{SrcType: TypeDBAccount, SrcID: a.ID, RelType: "belongs_to", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
		}
		pols, _ := s.Store.ListBackupPolicies(it.ID)
		for _, p := range pols {
			out = append(out, Edge{SrcType: TypeBackupPolicy, SrcID: p.ID, RelType: "protects", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
		}
		clusters, _ := s.Store.ListHAClusters("", "", "")
		for _, c := range clusters {
			members, _ := s.Store.ListHAMembers(c.ID)
			for _, m := range members {
				if m.InstanceID == it.ID {
					out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "member_of", DstType: TypeHACluster, DstID: c.ID, Source: "derived"})
					out = append(out, Edge{SrcType: TypeHACluster, SrcID: c.ID, RelType: "manages", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
				}
			}
		}
		if cid := srClusterIDFromTags(it.Tags); cid != "" {
			out = append(out, Edge{SrcType: TypeSrCluster, SrcID: cid, RelType: "owns", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
		}
		deploys, _ := s.Store.ListDbDeployClustersInTenant(it.TenantID, "")
		for _, c := range deploys {
			if c.LinkedInstanceID == it.ID {
				out = append(out, Edge{SrcType: TypeDbDeployCluster, SrcID: c.ID, RelType: "owns", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
			}
			if c.HaClusterID != "" {
				members, _ := s.Store.ListHAMembers(c.HaClusterID)
				for _, m := range members {
					if m.InstanceID == it.ID {
						out = append(out, Edge{SrcType: TypeDbDeployCluster, SrcID: c.ID, RelType: "owns", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
					}
				}
			}
		}
		domain := strings.TrimSpace(it.DomainName)
		if domain == "" && len(it.Domains) > 0 {
			domain = it.Domains[0]
		}
		if domain != "" {
			jobs, _ := s.Store.ListMonitorJobs("")
			for _, j := range jobs {
				groups, _ := s.Store.ListMonitorTargetGroups(j.ID)
				for _, g := range groups {
					if g.Labels["domain"] == domain || strings.Contains(g.Endpoints, domain) {
						out = append(out, Edge{SrcType: TypeMonitorGroup, SrcID: g.ID, RelType: "scrapes", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
					}
				}
			}
		}
		cdcs, _ := s.Store.ListCDCInTenant(it.TenantID, "", "")
		for _, t := range cdcs {
			if t.SourceID == it.ID {
				out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "data_flow", DstType: TypeCDCTask, DstID: t.ID, Source: "derived"})
				if t.TargetID != "" {
					out = append(out, Edge{SrcType: TypeCDCTask, SrcID: t.ID, RelType: "data_flow", DstType: TypeInstance, DstID: t.TargetID, Source: "derived"})
				}
			}
			if t.TargetID == it.ID {
				out = append(out, Edge{SrcType: TypeCDCTask, SrcID: t.ID, RelType: "data_flow", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
			}
		}
	case TypeHACluster:
		members, _ := s.Store.ListHAMembers(ciID)
		for _, m := range members {
			out = append(out, Edge{SrcType: TypeHACluster, SrcID: ciID, RelType: "manages", DstType: TypeInstance, DstID: m.InstanceID, Source: "derived"})
			out = append(out, Edge{SrcType: TypeInstance, SrcID: m.InstanceID, RelType: "member_of", DstType: TypeHACluster, DstID: ciID, Source: "derived"})
		}
	case TypeBackupPolicy:
		pList, _ := s.Store.ListBackupPolicies("")
		for _, p := range pList {
			if p.ID == ciID && p.InstanceID != "" {
				out = append(out, Edge{SrcType: TypeBackupPolicy, SrcID: p.ID, RelType: "protects", DstType: TypeInstance, DstID: p.InstanceID, Source: "derived"})
			}
		}
	case TypeHost:
		host := strings.TrimPrefix(ciID, "host:")
		allowIDs := s.srHostAllowInstanceIDs(ctx.SrClusterID)
		insts, _ := s.Store.ListInstances("", "")
		for _, it := range insts {
			h := strings.TrimSpace(it.Host)
			if hh, _, err := net.SplitHostPort(h); err == nil {
				h = hh
			}
			if h != host {
				continue
			}
			if ctx.SrClusterID != "" && !instanceAllowedOnSrHost(&it, ctx.SrClusterID, allowIDs) {
				continue
			}
			out = append(out, Edge{SrcType: TypeHost, SrcID: ciID, RelType: "runs", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
			out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "runs_on", DstType: TypeHost, DstID: ciID, Source: "derived"})
		}
	case TypeSrCluster:
		cl, err := s.Store.GetSrCluster(ciID)
		if err != nil || cl == nil {
			return out, err
		}
		nodes, _ := s.Store.ListSrNodes(ciID)
		for _, n := range nodes {
			if n.HostInstanceID != "" {
				out = append(out, Edge{SrcType: TypeSrCluster, SrcID: ciID, RelType: "runs_on", DstType: TypeInstance, DstID: n.HostInstanceID, Source: "derived"})
			}
			host := strings.TrimSpace(n.Host)
			if host != "" {
				out = append(out, Edge{SrcType: TypeSrCluster, SrcID: ciID, RelType: "runs_on", DstType: TypeHost, DstID: "host:" + host, Source: "derived"})
			}
		}
		insts, _ := s.Store.ListInstancesInTenant(cl.TenantID, "", "")
		for _, it := range insts {
			if srClusterIDFromTags(it.Tags) == ciID {
				out = append(out, Edge{SrcType: TypeSrCluster, SrcID: ciID, RelType: "owns", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
			}
		}
	case TypeDbDeployCluster:
		cl, err := s.Store.GetDbDeployCluster(ciID)
		if err != nil || cl == nil {
			return out, err
		}
		if cl.LinkedInstanceID != "" {
			out = append(out, Edge{SrcType: TypeDbDeployCluster, SrcID: ciID, RelType: "owns", DstType: TypeInstance, DstID: cl.LinkedInstanceID, Source: "derived"})
		}
		if cl.HaClusterID != "" {
			out = append(out, Edge{SrcType: TypeDbDeployCluster, SrcID: ciID, RelType: "uses_ha", DstType: TypeHACluster, DstID: cl.HaClusterID, Source: "derived"})
		}
		nodes, _ := s.Store.ListDbDeployNodes(ciID)
		for _, n := range nodes {
			if n.HostInstanceID != "" {
				out = append(out, Edge{SrcType: TypeDbDeployCluster, SrcID: ciID, RelType: "runs_on", DstType: TypeInstance, DstID: n.HostInstanceID, Source: "derived"})
			}
			host := strings.TrimSpace(n.Host)
			if host != "" {
				out = append(out, Edge{SrcType: TypeDbDeployCluster, SrcID: ciID, RelType: "runs_on", DstType: TypeHost, DstID: "host:" + host, Source: "derived"})
				if n.Port > 0 {
					if it, _ := s.Store.FindInstanceByHostPortInTenant(cl.TenantID, cl.Engine, n.Host, n.Port); it != nil {
						out = append(out, Edge{SrcType: TypeDbDeployCluster, SrcID: ciID, RelType: "owns", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
					}
				}
			}
		}
	case TypeProxyService:
		p, err := s.Store.GetProxyService(ciID)
		if err != nil || p == nil {
			return out, err
		}
		if p.EnvID != "" {
			out = append(out, Edge{SrcType: TypeProxyService, SrcID: p.ID, RelType: "in_env", DstType: TypeEnvironment, DstID: p.EnvID, Source: "derived"})
		}
	case TypeDBAccount:
		accs, _ := s.Store.ListAccounts("", "", "", "")
		for _, a := range accs {
			if a.ID != ciID {
				continue
			}
			if a.InstanceID != "" {
				out = append(out, Edge{SrcType: TypeDBAccount, SrcID: a.ID, RelType: "belongs_to", DstType: TypeInstance, DstID: a.InstanceID, Source: "derived"})
			}
			if a.DatabaseID != "" {
				out = append(out, Edge{SrcType: TypeDatabase, SrcID: a.DatabaseID, RelType: "has_account", DstType: TypeDBAccount, DstID: a.ID, Source: "derived"})
			}
			break
		}
	case TypeCDCTask:
		list, _ := s.Store.ListCDCInTenant("", "", "")
		for _, t := range list {
			if t.ID != ciID {
				continue
			}
			if t.SourceID != "" {
				out = append(out, Edge{SrcType: TypeInstance, SrcID: t.SourceID, RelType: "data_flow", DstType: TypeCDCTask, DstID: t.ID, Source: "derived"})
			}
			if t.TargetID != "" {
				out = append(out, Edge{SrcType: TypeCDCTask, SrcID: t.ID, RelType: "data_flow", DstType: TypeInstance, DstID: t.TargetID, Source: "derived"})
			}
			break
		}
	}
	return out, nil
}

// srHostAllowInstanceIDs 集群节点绑定的 host 实例 ID。
func (s *Service) srHostAllowInstanceIDs(srClusterID string) map[string]bool {
	out := map[string]bool{}
	srClusterID = strings.TrimSpace(srClusterID)
	if srClusterID == "" {
		return out
	}
	nodes, _ := s.Store.ListSrNodes(srClusterID)
	for _, n := range nodes {
		if id := strings.TrimSpace(n.HostInstanceID); id != "" {
			out[id] = true
		}
	}
	return out
}

func instanceAllowedOnSrHost(it *store.Instance, srClusterID string, allowIDs map[string]bool) bool {
	if it == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(it.DBType), "host") {
		return true
	}
	if allowIDs[it.ID] {
		return true
	}
	return srClusterIDFromTags(it.Tags) == srClusterID
}

func srClusterIDFromTags(tags []string) string {
	const prefix = "sr-cluster="
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToLower(t), prefix) {
			return strings.TrimSpace(t[len(prefix):])
		}
	}
	return ""
}
