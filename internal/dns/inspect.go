package dns

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// HostRef 平台已知的主机地址或域名（来自实例的 host 与访问域名）。
type HostRef struct {
	Addr         string `json:"addr"`
	InstanceID   string `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	DBType       string `json:"db_type"`
}

// Inspect 全量体检：规则校验加上与实例的比对。
// 悬空记录：A/AAAA 的 IP 与完整域名都不对应任何实例。
// 实例域名缺失：实例地址是受管域下的域名，但没有运行中的记录。
func Inspect(s *Snapshot, refs []HostRef) []Issue {
	out := Lint(s)
	if len(refs) == 0 {
		return out
	}
	known := map[string]bool{}
	for _, r := range refs {
		known[normName(r.Addr)] = true
	}
	origins := map[string]string{}
	var managed []string
	for _, z := range s.Zones {
		if z.Status == StatusActive {
			origins[z.ID] = z.Origin
			managed = append(managed, normName(z.Origin))
		}
	}
	have := map[string]bool{}
	for _, r := range s.Records {
		o, ok := origins[r.ZoneID]
		if !ok || r.Status != StatusActive {
			continue
		}
		fq := FQDN(r.Name, o)
		have[fq] = true
		if (r.Type == "A" || r.Type == "AAAA") && !known[normName(r.Data)] && !known[fq] {
			out = append(out, Issue{
				Level: LevelInfo, Code: "dangling_record", RecordIDs: []string{r.ID}, Names: []string{fq},
				Message: fmt.Sprintf("%s → %s 没有对应的实例（也可能是非数据库用途）", fq, r.Data),
			})
		}
	}
	reported := map[string]bool{}
	for _, ref := range refs {
		n := normName(ref.Addr)
		if n == "" || net.ParseIP(n) != nil || reported[n] || have[n] {
			continue
		}
		if managedOrigin(n, managed) == "" {
			continue
		}
		reported[n] = true
		out = append(out, Issue{
			Level: LevelWarn, Code: "instance_domain_missing", Names: []string{n},
			Message: fmt.Sprintf("实例 %s 使用的域名 %s 在受管域内没有运行中的记录", ref.InstanceName, n),
		})
	}
	return out
}

type ReverseResult struct {
	IP      string       `json:"ip"`
	Records []RecordSnap `json:"records"`
	CNAMEs  []RecordSnap `json:"cnames"`
	Names   []string     `json:"names"`
}

// Reverse 指向某 IP 的 A/AAAA 记录，以及（逐级）指向这些名字的 CNAME。
func Reverse(s *Snapshot, ip string) (*ReverseResult, error) {
	p := net.ParseIP(strings.TrimSpace(ip))
	if p == nil {
		return nil, fmt.Errorf("IP 无效")
	}
	out := &ReverseResult{IP: p.String(), Records: []RecordSnap{}, CNAMEs: []RecordSnap{}, Names: []string{}}
	origins := map[string]string{}
	for _, z := range s.Zones {
		origins[z.ID] = z.Origin
	}
	names := map[string]bool{}
	for _, r := range s.Records {
		if (r.Type == "A" || r.Type == "AAAA") && net.ParseIP(r.Data) != nil && net.ParseIP(r.Data).Equal(p) {
			out.Records = append(out.Records, r)
			names[FQDN(r.Name, origins[r.ZoneID])] = true
		}
	}
	used := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, r := range s.Records {
			if r.Type != "CNAME" || used[r.ID] || !names[normName(r.Data)] {
				continue
			}
			used[r.ID] = true
			out.CNAMEs = append(out.CNAMEs, r)
			fq := FQDN(r.Name, origins[r.ZoneID])
			if !names[fq] {
				names[fq] = true
				changed = true
			}
		}
	}
	for n := range names {
		out.Names = append(out.Names, n)
	}
	sort.Strings(out.Names)
	return out, nil
}
