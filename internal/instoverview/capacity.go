package instoverview

import (
	"math"
	"strings"

	"auto-dba/internal/store"
)

// CapacityHit 是一条按 idc、domain、cname 汇总后的磁盘容量，单位字节。
type CapacityHit struct {
	IDC    string
	Domain string
	CName  string
	Bytes  float64
}

// StarRocksIDCAliases 给出能对上 Grafana idc 的值。
// 标签管理里配置了 idc 候选值时，只取等于环境编码、或以「编码-」开头的那些（如 beijing 与 beijing-t）。
// 没有候选值时只用环境编码和实例上的 idc。实例自己的 idc= 标签始终保留。
func StarRocksIDCAliases(envCode, instIDC string, tags, defined []string) []string {
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
	for _, raw := range tags {
		k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "idc") {
			add(v)
		}
	}
	anchor := strings.TrimSpace(envCode)
	if anchor == "" {
		anchor = strings.TrimSpace(instIDC)
	}
	if len(compactDefined(defined)) == 0 {
		add(envCode)
		add(instIDC)
		return out
	}
	anchorL := strings.ToLower(anchor)
	prefix := anchorL + "-"
	for _, v := range defined {
		vl := strings.ToLower(strings.TrimSpace(v))
		if vl == "" || anchorL == "" {
			continue
		}
		if vl == anchorL || strings.HasPrefix(vl, prefix) {
			add(v)
		}
	}
	return out
}

// StarRocksHostCandidates 是实例地址、域名，以及 domain、cname、host 标签。
func StarRocksHostCandidates(inst *store.Instance) []string {
	if inst == nil {
		return nil
	}
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
	add(inst.Host)
	add(inst.DomainName)
	for _, d := range inst.Domains {
		add(d)
	}
	for _, raw := range inst.Tags {
		k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "domain", "cname", "host":
			add(v)
		}
	}
	return out
}

// SumCapacity 把 idc 与主机同时命中的序列相加。cname 标签未定义时不看序列上的 cname。
func SumCapacity(idcs, hosts []string, allowCName bool, hits []CapacityHit) (float64, bool) {
	if len(idcs) == 0 || len(hosts) == 0 {
		return 0, false
	}
	var sum float64
	hit := false
	for _, h := range hits {
		if !containsFold(idcs, h.IDC) {
			continue
		}
		if !containsFold(hosts, h.Domain) && !(allowCName && containsFold(hosts, h.CName)) {
			continue
		}
		sum += h.Bytes
		hit = true
	}
	return sum, hit
}

// ApplyPromCapacity 用同一批指标覆盖数据存储和数据盘，并重算使用率。盘容量不是正数时不改原来的采样。
func ApplyPromCapacity(s *Sample, dataBytes, diskBytes float64) {
	if s == nil || diskBytes <= 0 || math.IsNaN(dataBytes) || math.IsNaN(diskBytes) {
		return
	}
	s.DataBytes = int64(math.Round(dataBytes))
	s.DataKnown = true
	s.DiskBytes = int64(math.Round(diskBytes))
	s.DiskKnown = true
	s.StorageRate = dataBytes / diskBytes * 100
	s.StorageRateKnown = true
}

func compactDefined(in []string) []string {
	var out []string
	for _, v := range in {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func containsFold(list []string, v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	for _, e := range list {
		if strings.EqualFold(strings.TrimSpace(e), v) {
			return true
		}
	}
	return false
}
