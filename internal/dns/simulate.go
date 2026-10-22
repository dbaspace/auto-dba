package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	RcodeNoError  = "NOERROR"
	RcodeNXDomain = "NXDOMAIN"
	RcodeForward  = "FORWARD"
	RcodeError    = "ERROR"
)

type SimAnswer struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data string `json:"data"`
	TTL  int64  `json:"ttl"`
}

type SimResult struct {
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	SourceIP  string      `json:"source_ip"`
	RangeID   string      `json:"range_id"`
	RangeName string      `json:"range_name"`
	Rcode     string      `json:"rcode"`
	Forward   string      `json:"forward,omitempty"`
	Answers   []SimAnswer `json:"answers"`
	Steps     []string    `json:"steps"`
}

const maxCNAMEHops = 8

// Simulate 按 hdns 的规则推算应答：来源 IP 命中解析范围（按读出顺序取第一个），
// 未命中时用 DEFAULT；在范围内查记录并追 CNAME；不在受管域的名字转发上游。
func Simulate(s *Snapshot, name, qtype, sourceIP string) (*SimResult, error) {
	qn := normName(name)
	if qn == "" {
		return nil, fmt.Errorf("请填写域名")
	}
	qt, err := NormalizeType(qtype)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(sourceIP))
	if ip == nil {
		return nil, fmt.Errorf("来源 IP 无效")
	}
	res := &SimResult{Name: qn, Type: qt, SourceIP: ip.String(), Answers: []SimAnswer{}}
	step := func(format string, args ...any) { res.Steps = append(res.Steps, fmt.Sprintf(format, args...)) }

	for _, r := range s.Ranges {
		if r.Name == DefaultRangeName || r.Status != StatusActive {
			continue
		}
		for _, c := range r.NetworkList {
			_, n, err := net.ParseCIDR(strings.TrimSpace(c))
			if err == nil && n.Contains(ip) {
				res.RangeID, res.RangeName = r.ID, r.Name
				step("来源 %s 命中解析范围 %s（%s）", ip, r.Name, c)
				break
			}
		}
		if res.RangeID != "" {
			break
		}
	}
	if res.RangeID == "" {
		res.RangeID, res.RangeName = DefaultRangeID, DefaultRangeName
		step("来源 %s 未命中任何解析范围，使用 DEFAULT", ip)
	}

	var managed []string
	for _, z := range s.Zones {
		if z.Status == StatusActive {
			managed = append(managed, normName(z.Origin))
		}
	}
	byName := map[string][]RecordSnap{}
	origins := map[string]string{}
	for _, z := range s.Zones {
		if z.Status == StatusActive {
			origins[z.ID] = z.Origin
		}
	}
	for _, r := range s.Records {
		o, ok := origins[r.ZoneID]
		if !ok || r.Status != StatusActive || r.RangeID != res.RangeID {
			continue
		}
		fq := FQDN(r.Name, o)
		byName[fq] = append(byName[fq], r)
	}

	cur := qn
	visited := map[string]bool{}
	for hop := 0; ; hop++ {
		if hop > maxCNAMEHops || visited[cur] {
			res.Rcode = RcodeError
			step("CNAME 链过长或成环，停止")
			return res, nil
		}
		visited[cur] = true
		zone := managedOrigin(cur, managed)
		if zone == "" {
			res.Rcode = RcodeForward
			res.Forward = cur
			step("%s 不在受管域内，转发上游解析", cur)
			return res, nil
		}
		recs := byName[cur]
		var hit, cnames []RecordSnap
		for _, r := range recs {
			if r.Type == qt {
				hit = append(hit, r)
			} else if r.Type == "CNAME" {
				cnames = append(cnames, r)
			}
		}
		if len(hit) > 0 {
			sort.Slice(hit, func(i, j int) bool { return hit[i].Data < hit[j].Data })
			for _, r := range hit {
				res.Answers = append(res.Answers, SimAnswer{Name: cur, Type: r.Type, Data: displayData(r), TTL: r.TTL})
			}
			res.Rcode = RcodeNoError
			step("域 %s 内 %s 有 %d 条 %s 记录", zone, cur, len(hit), qt)
			return res, nil
		}
		if len(cnames) > 0 {
			t := normName(cnames[0].Data)
			res.Answers = append(res.Answers, SimAnswer{Name: cur, Type: "CNAME", Data: t, TTL: cnames[0].TTL})
			step("%s 为 CNAME，继续解析 %s", cur, t)
			cur = t
			continue
		}
		if len(recs) > 0 {
			res.Rcode = RcodeNoError
			step("%s 有记录但没有 %s 类型，返回空应答", cur, qt)
			return res, nil
		}
		res.Rcode = RcodeNXDomain
		step("域 %s 内 %s 范围没有 %s 的记录，返回 NXDOMAIN", zone, res.RangeName, cur)
		return res, nil
	}
}

func displayData(r RecordSnap) string {
	if r.Type == "MX" {
		if h, p, ok := DecodeMX(r.Data); ok {
			return strconv.Itoa(p) + " " + normName(h)
		}
	}
	if r.Type == "CNAME" {
		return normName(r.Data)
	}
	return r.Data
}

type ProbeResult struct {
	Server  string   `json:"server"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Rcode   string   `json:"rcode"`
	Answers []string `json:"answers"`
	Error   string   `json:"error,omitempty"`
	Elapsed int64    `json:"elapsed_ms"`
}

// Probe 向指定 DNS 服务器实查。来源 IP 为本机出口地址。
func Probe(ctx context.Context, server, name, qtype string) *ProbeResult {
	start := time.Now()
	res := &ProbeResult{Server: server, Name: normName(name), Type: strings.ToUpper(strings.TrimSpace(qtype)), Answers: []string{}}
	defer func() { res.Elapsed = time.Since(start).Milliseconds() }()
	if _, _, err := net.SplitHostPort(server); err != nil {
		res.Rcode, res.Error = RcodeError, "服务器地址须为 host:port"
		return res
	}
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, server)
		},
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := res.Name + "."
	var err error
	switch res.Type {
	case "A", "AAAA":
		netw := "ip4"
		if res.Type == "AAAA" {
			netw = "ip6"
		}
		var ips []net.IP
		ips, err = r.LookupIP(ctx, netw, q)
		for _, ip := range ips {
			res.Answers = append(res.Answers, ip.String())
		}
	case "CNAME":
		var c string
		c, err = r.LookupCNAME(ctx, q)
		if err == nil && normName(c) != res.Name {
			res.Answers = append(res.Answers, normName(c))
		}
	case "TXT":
		res.Answers, err = r.LookupTXT(ctx, q)
	case "MX":
		var mx []*net.MX
		mx, err = r.LookupMX(ctx, q)
		for _, m := range mx {
			res.Answers = append(res.Answers, strconv.Itoa(int(m.Pref))+" "+normName(m.Host))
		}
	default:
		res.Rcode, res.Error = RcodeError, "记录类型仅支持 A、AAAA、CNAME、TXT、MX"
		return res
	}
	if err != nil {
		var de *net.DNSError
		if errors.As(err, &de) && de.IsNotFound {
			res.Rcode = RcodeNXDomain
			return res
		}
		res.Rcode, res.Error = RcodeError, err.Error()
		return res
	}
	sort.Strings(res.Answers)
	res.Rcode = RcodeNoError
	return res
}
