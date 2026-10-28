package instoverview

import (
	"fmt"
	"strings"
)

// CPUSeriesHit 是一条 redis_exporter CPU 时序（百分比点列）。
type CPUSeriesHit struct {
	Instance string
	Points   []CpuPoint
}

// RedisCPUCandidates 生成 Prometheus instance 标签候选（host:port / 域名:port / 裸 host）。
func RedisCPUCandidates(host string, port int, domains []string) []string {
	host = strings.TrimSpace(host)
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		for _, e := range out {
			if strings.EqualFold(e, v) {
				return
			}
		}
		out = append(out, v)
	}
	if host != "" {
		if port > 0 {
			add(fmt.Sprintf("%s:%d", host, port))
		}
		add(host)
	}
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if port > 0 {
			add(fmt.Sprintf("%s:%d", d, port))
		}
		add(d)
	}
	return out
}

// PickRedisCPU 按 instance 标签匹配；优先完整 host:port，命中多条时取候选顺序第一条。
func PickRedisCPU(host string, port int, domains []string, hits []CPUSeriesHit) ([]CpuPoint, bool) {
	if len(hits) == 0 {
		return nil, false
	}
	byInst := map[string][]CpuPoint{}
	for _, h := range hits {
		key := strings.ToLower(strings.TrimSpace(h.Instance))
		if key == "" || len(h.Points) == 0 {
			continue
		}
		byInst[key] = h.Points
	}
	for _, c := range RedisCPUCandidates(host, port, domains) {
		if pts, ok := byInst[strings.ToLower(c)]; ok {
			return pts, true
		}
	}
	return nil, false
}
