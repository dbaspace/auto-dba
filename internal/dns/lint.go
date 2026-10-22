package dns

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

const (
	LevelError = "error"
	LevelWarn  = "warn"
	LevelInfo  = "info"
)

// Issue 校验或体检发现的问题。Key 为 域|范围|主机名，Names 为涉及的完整域名。
type Issue struct {
	Level     string   `json:"level"`
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	RecordIDs []string `json:"record_ids,omitempty"`
	Key       string   `json:"-"`
	Names     []string `json:"names,omitempty"`
}

// MinTTL 低于该值提示。预降 TTL 默认值与之相同。
const MinTTL = 30

// Lint 对 Active 记录做规则校验。只看解析域为 Active 的记录。
func Lint(s *Snapshot) []Issue {
	if s == nil {
		return nil
	}
	origins := map[string]string{}
	var managed []string
	for _, z := range s.Zones {
		if z.Status != StatusActive {
			continue
		}
		origins[z.ID] = z.Origin
		managed = append(managed, normName(z.Origin))
	}
	rangeName := map[string]string{}
	var nets []*net.IPNet
	for _, r := range s.Ranges {
		rangeName[r.ID] = r.Name
		if r.Status != StatusActive {
			continue
		}
		for _, c := range r.NetworkList {
			if _, n, err := net.ParseCIDR(strings.TrimSpace(c)); err == nil {
				nets = append(nets, n)
			}
		}
	}
	type group struct {
		zoneID, rangeID, name string
		recs                  []RecordSnap
	}
	groups := map[string]*group{}
	var order []string
	// 每个解析范围内：完整域名 → 该名字的记录
	byName := map[string]map[string][]RecordSnap{}
	for _, r := range s.Records {
		origin, ok := origins[r.ZoneID]
		if !ok || r.Status != StatusActive {
			continue
		}
		k := recordKey(r.ZoneID, r.RangeID, r.Name)
		g := groups[k]
		if g == nil {
			g = &group{zoneID: r.ZoneID, rangeID: r.RangeID, name: r.Name}
			groups[k] = g
			order = append(order, k)
		}
		g.recs = append(g.recs, r)
		if byName[r.RangeID] == nil {
			byName[r.RangeID] = map[string][]RecordSnap{}
		}
		fq := FQDN(r.Name, origin)
		byName[r.RangeID][fq] = append(byName[r.RangeID][fq], r)
	}
	sort.Strings(order)
	var out []Issue
	for _, k := range order {
		g := groups[k]
		fq := FQDN(g.name, origins[g.zoneID])
		var cnames, others []RecordSnap
		for _, r := range g.recs {
			if r.Type == "CNAME" {
				cnames = append(cnames, r)
			} else {
				others = append(others, r)
			}
		}
		if len(cnames) > 0 && len(others) > 0 {
			out = append(out, Issue{
				Level: LevelError, Code: "cname_conflict", Key: k, Names: []string{fq},
				RecordIDs: ids(g.recs),
				Message:   fmt.Sprintf("%s（%s）同时存在 CNAME 与 %s 记录", fq, rangeName[g.rangeID], typesOf(others)),
			})
		}
		if len(cnames) > 1 {
			out = append(out, Issue{
				Level: LevelError, Code: "cname_multi", Key: k, Names: []string{fq}, RecordIDs: ids(cnames),
				Message: fmt.Sprintf("%s（%s）有 %d 条 CNAME", fq, rangeName[g.rangeID], len(cnames)),
			})
		}
	}
	for _, r := range s.Records {
		origin, ok := origins[r.ZoneID]
		if !ok || r.Status != StatusActive {
			continue
		}
		fq := FQDN(r.Name, origin)
		k := recordKey(r.ZoneID, r.RangeID, r.Name)
		target := ""
		switch r.Type {
		case "CNAME":
			target = r.Data
		case "MX":
			if h, _, ok := DecodeMX(r.Data); ok {
				target = h
			}
		case "A", "AAAA":
			if ip := net.ParseIP(r.Data); ip != nil && len(nets) > 0 && !inNets(ip, nets) {
				out = append(out, Issue{
					Level: LevelInfo, Code: "ip_outside_ranges", Key: k, Names: []string{fq}, RecordIDs: []string{r.ID},
					Message: fmt.Sprintf("%s → %s 不在任何解析范围的来源网段内", fq, r.Data),
				})
			}
		}
		if r.TTL < MinTTL {
			out = append(out, Issue{
				Level: LevelWarn, Code: "ttl_low", Key: k, Names: []string{fq}, RecordIDs: []string{r.ID},
				Message: fmt.Sprintf("%s %s TTL 仅 %d 秒", fq, r.Type, r.TTL),
			})
		}
		if target == "" {
			continue
		}
		if net.ParseIP(strings.TrimSuffix(strings.TrimSpace(target), ".")) != nil {
			out = append(out, Issue{
				Level: LevelError, Code: "target_is_ip", Key: k, Names: []string{fq}, RecordIDs: []string{r.ID},
				Message: fmt.Sprintf("%s 的 %s 指向 IP %s，应指向域名", fq, r.Type, target),
			})
			continue
		}
		t := normName(target)
		if inManaged(t, managed) && len(byName[r.RangeID][t]) == 0 {
			out = append(out, Issue{
				Level: LevelWarn, Code: "target_missing", Key: k, Names: []string{fq, t}, RecordIDs: []string{r.ID},
				Message: fmt.Sprintf("%s 的 %s 指向 %s，但该名字在 %s 范围内没有记录", fq, r.Type, t, rangeName[r.RangeID]),
			})
		}
	}
	out = append(out, cnameLoops(byName, rangeName)...)
	return out
}

// cnameLoops 同一解析范围内沿 CNAME 追踪，出现回到已访问名字即为循环。
func cnameLoops(byName map[string]map[string][]RecordSnap, rangeName map[string]string) []Issue {
	var out []Issue
	rids := make([]string, 0, len(byName))
	for rid := range byName {
		rids = append(rids, rid)
	}
	sort.Strings(rids)
	for _, rid := range rids {
		names := byName[rid]
		next := map[string]string{}
		recOf := map[string]string{}
		for fq, recs := range names {
			for _, r := range recs {
				if r.Type == "CNAME" {
					next[fq] = normName(r.Data)
					recOf[fq] = r.ID
				}
			}
		}
		seen := map[string]bool{}
		starts := make([]string, 0, len(next))
		for fq := range next {
			starts = append(starts, fq)
		}
		sort.Strings(starts)
		for _, start := range starts {
			if seen[start] {
				continue
			}
			path := []string{}
			onPath := map[string]int{}
			cur := start
			for {
				if i, ok := onPath[cur]; ok {
					loop := path[i:]
					var recIDs []string
					for _, n := range loop {
						recIDs = append(recIDs, recOf[n])
					}
					out = append(out, Issue{
						Level: LevelError, Code: "cname_loop", Names: loop, RecordIDs: recIDs,
						Message: fmt.Sprintf("%s 范围内 CNAME 循环：%s → %s", rangeName[rid], strings.Join(loop, " → "), loop[0]),
					})
					break
				}
				if seen[cur] {
					break
				}
				nx, ok := next[cur]
				if !ok {
					break
				}
				onPath[cur] = len(path)
				path = append(path, cur)
				cur = nx
			}
			for _, n := range path {
				seen[n] = true
			}
		}
	}
	return out
}

func inNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// inManaged 名字是否落在某个受管域下。
func inManaged(name string, managed []string) bool {
	return managedOrigin(name, managed) != ""
}

// managedOrigin 返回最长匹配的受管域；不在任何域下返回空。
func managedOrigin(name string, managed []string) string {
	best := ""
	for _, o := range managed {
		if name == o || strings.HasSuffix(name, "."+o) {
			if len(o) > len(best) {
				best = o
			}
		}
	}
	return best
}

func ids(recs []RecordSnap) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return out
}

func typesOf(recs []RecordSnap) string {
	seen := map[string]bool{}
	var out []string
	for _, r := range recs {
		if !seen[r.Type] {
			seen[r.Type] = true
			out = append(out, r.Type)
		}
	}
	return strings.Join(out, "、")
}
