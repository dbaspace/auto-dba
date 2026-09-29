package cmdb

import (
	"fmt"
	"net"
	"strings"
)

// BuildGraph 推导关系 + 显式关系，depth=1|2。
func (s *Service) BuildGraph(ciType, ciID string, depth int) (*Graph, error) {
	if depth < 1 {
		depth = 1
	}
	if depth > 2 {
		depth = 2
	}
	root, err := s.GetCI(ciType, ciID)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("配置项不存在")
	}
	nodes := map[string]CI{nodeKey(root.Type, root.ID): *root}
	var edges []Edge

	addNode := func(c CI) {
		if c.ID == "" {
			return
		}
		nodes[nodeKey(c.Type, c.ID)] = c
	}
	addEdge := func(e Edge) {
		edges = append(edges, e)
	}

	if err := s.collectNeighbors(root.Type, root.ID, depth, addNode, addEdge); err != nil {
		return nil, err
	}

	outNodes := make([]CI, 0, len(nodes))
	for _, n := range nodes {
		outNodes = append(outNodes, n)
	}
	return &Graph{Nodes: outNodes, Edges: edges}, nil
}

func nodeKey(t, id string) string { return t + "|" + id }

func (s *Service) collectNeighbors(ciType, ciID string, depth int, addNode func(CI), addEdge func(Edge)) error {
	derived, err := s.derivedEdges(ciType, ciID)
	if err != nil {
		return err
	}
	manual, err := s.Store.ListCmdbRelations(ciType, ciID, "", "")
	if err != nil {
		return err
	}
	manual2, err := s.Store.ListCmdbRelations("", "", ciType, ciID)
	if err != nil {
		return err
	}

	seen := map[string]bool{}
	queue := []struct {
		t, id string
		d     int
	}{{ciType, ciID, 0}}

	processEdges := func(srcType, srcID string) ([]Edge, error) {
		var out []Edge
		d1, err := s.derivedEdges(srcType, srcID)
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
	_ = derived
	_ = manual
	_ = manual2

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

func (s *Service) derivedEdges(ciType, ciID string) ([]Edge, error) {
	var out []Edge
	switch ciType {
	case TypeDatabase:
		db, err := s.Store.GetDatabase(ciID)
		if err != nil || db == nil {
			return out, err
		}
		out = append(out, Edge{SrcType: TypeDatabase, SrcID: db.ID, RelType: "hosted_on", DstType: TypeInstance, DstID: db.InstanceID, Source: "derived"})
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
		dbs, _ := s.Store.ListDatabases(it.ID, "")
		for _, db := range dbs {
			out = append(out, Edge{SrcType: TypeDatabase, SrcID: db.ID, RelType: "hosted_on", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
		}
		accs, _ := s.Store.ListAccounts("", "", it.ID, "")
		for _, a := range accs {
			out = append(out, Edge{SrcType: "account", SrcID: a.ID, RelType: "belongs_to", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
		}
		pols, _ := s.Store.ListBackupPolicies(it.ID)
		for _, p := range pols {
			out = append(out, Edge{SrcType: TypeBackupPolicy, SrcID: p.ID, RelType: "protects", DstType: TypeInstance, DstID: it.ID, Source: "derived"})
		}
		// HA membership
		clusters, _ := s.Store.ListHAClusters("", "", "")
		for _, c := range clusters {
			members, _ := s.Store.ListHAMembers(c.ID)
			for _, m := range members {
				if m.InstanceID == it.ID {
					out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "member_of", DstType: TypeHACluster, DstID: c.ID, Source: "derived"})
				}
			}
		}
		// weak monitor match by domain
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
	case TypeHACluster:
		members, _ := s.Store.ListHAMembers(ciID)
		for _, m := range members {
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
		insts, _ := s.Store.ListInstances("", "")
		for _, it := range insts {
			h := strings.TrimSpace(it.Host)
			if hh, _, err := net.SplitHostPort(h); err == nil {
				h = hh
			}
			if h == host {
				out = append(out, Edge{SrcType: TypeInstance, SrcID: it.ID, RelType: "runs_on", DstType: TypeHost, DstID: ciID, Source: "derived"})
			}
		}
	}
	return out, nil
}
