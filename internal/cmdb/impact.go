package cmdb

import (
	"fmt"
	"strings"
)

// ImpactReport 影响分析结果。
type ImpactReport struct {
	Root      CI            `json:"root"`
	Direction string        `json:"direction"` // up | down | both
	Depth     int           `json:"depth"`
	Summary   ImpactSummary `json:"summary"`
	Nodes     []CI          `json:"nodes"`
	Edges     []Edge        `json:"edges"`
}

type ImpactSummary struct {
	Upstream   int            `json:"upstream"`
	Downstream int            `json:"downstream"`
	ByType     map[string]int `json:"by_type"`
	AppTags    []string       `json:"app_tags,omitempty"`
	BizTags    []string       `json:"biz_tags,omitempty"`
}

// ImpactInTenant 基于图 BFS 做上下游影响分析。
// direction: up=依赖方/承载方, down=受影响方, both=双向。
func (s *Service) ImpactInTenant(ciType, ciID, tenantID, direction string, depth int) (*ImpactReport, error) {
	direction = strings.ToLower(strings.TrimSpace(direction))
	if direction == "" {
		direction = "down"
	}
	if direction != "up" && direction != "down" && direction != "both" {
		return nil, fmt.Errorf("direction 须为 up|down|both")
	}
	if depth < 1 {
		depth = 2
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
	upCount, downCount := 0, 0
	byType := map[string]int{}

	seen := map[string]bool{nodeKey(root.Type, root.ID): true}
	queue := []struct {
		t, id string
		d     int
	}{{root.Type, root.ID, 0}}
	expandCtx := s.resolveExpandCtx(root.Type, root.ID)

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.d >= depth {
			continue
		}
		eds, err := s.derivedEdges(cur.t, cur.id, expandCtx)
		if err != nil {
			return nil, err
		}
		m1, _ := s.Store.ListCmdbRelations(cur.t, cur.id, "", "")
		for _, r := range m1 {
			eds = append(eds, Edge{SrcType: r.SrcType, SrcID: r.SrcID, RelType: r.RelType, DstType: r.DstType, DstID: r.DstID, Source: r.Source, Remark: r.Remark})
		}
		m2, _ := s.Store.ListCmdbRelations("", "", cur.t, cur.id)
		for _, r := range m2 {
			eds = append(eds, Edge{SrcType: r.SrcType, SrcID: r.SrcID, RelType: r.RelType, DstType: r.DstType, DstID: r.DstID, Source: r.Source, Remark: r.Remark})
		}
		for _, e := range eds {
			ek := e.SrcType + "|" + e.SrcID + "|" + e.RelType + "|" + e.DstType + "|" + e.DstID
			var peerType, peerID string
			var isDown bool
			if e.SrcType == cur.t && e.SrcID == cur.id {
				peerType, peerID = e.DstType, e.DstID
				isDown = true
			} else if e.DstType == cur.t && e.DstID == cur.id {
				peerType, peerID = e.SrcType, e.SrcID
				isDown = false
			} else {
				continue
			}
			if direction == "down" && !isDown {
				continue
			}
			if direction == "up" && isDown {
				continue
			}
			if !seenEdge[ek] {
				seenEdge[ek] = true
				edges = append(edges, e)
			}
			pk := nodeKey(peerType, peerID)
			if seen[pk] {
				continue
			}
			seen[pk] = true
			ci, _ := s.GetCI(peerType, peerID)
			if ci == nil {
				ci = &CI{ID: peerID, Type: peerType, Name: peerID}
			}
			nodes[pk] = *ci
			byType[ci.Type]++
			if isDown {
				downCount++
			} else {
				upCount++
			}
			queue = append(queue, struct {
				t, id string
				d     int
			}{peerType, peerID, cur.d + 1})
		}
	}

	outNodes := make([]CI, 0, len(nodes))
	appTags, bizTags := map[string]bool{}, map[string]bool{}
	for _, n := range nodes {
		outNodes = append(outNodes, n)
		for _, t := range n.Tags {
			tl := strings.ToLower(strings.TrimSpace(t))
			if strings.HasPrefix(tl, "app=") {
				appTags[t] = true
			}
			if strings.HasPrefix(tl, "biz=") {
				bizTags[t] = true
			}
		}
		if n.Attrs != nil {
			// also scan attrs tags if present in Tags only
		}
	}
	sum := ImpactSummary{
		Upstream: upstreamOr(direction, upCount), Downstream: downstreamOr(direction, downCount),
		ByType: byType, AppTags: keysOf(appTags), BizTags: keysOf(bizTags),
	}
	return &ImpactReport{Root: *root, Direction: direction, Depth: depth, Summary: sum, Nodes: outNodes, Edges: edges}, nil
}

func upstreamOr(dir string, n int) int {
	if dir == "down" {
		return 0
	}
	return n
}

func downstreamOr(dir string, n int) int {
	if dir == "up" {
		return 0
	}
	return n
}

func keysOf(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
