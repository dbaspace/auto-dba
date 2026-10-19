package alert

import (
	"net"
	"strings"

	"auto-dba/internal/store"
)

// 告警只发给平台已登记的资源；补充到告警上的登记信息标签（不参与指纹计算）。
const (
	LabelRegisteredName = "registered_name"
	LabelRegisteredHost = "registered_host"
	LabelRegisteredType = "registered_type"
)

// 规则 service 为这些类型时，必须命中同类型的登记资源。
var strictAlertTypes = map[string]bool{"host": true, "postgres": true, "mysql": true}

// seriesAddrLabels 采集序列中可用于对应登记资源的标签。
var seriesAddrLabels = []string{"instance", "hostname", "host", "d_b_instance_name", "domain", "server", "ip"}

func normalizeAlertType(t string) string {
	n := strings.ToLower(strings.TrimSpace(t))
	switch n {
	case "postgresql", "pg", "pgsql":
		return "postgres"
	case "os", "主机", "node":
		return "host"
	case "mssql":
		return "sqlserver"
	case "mongo":
		return "mongodb"
	}
	return n
}

func stripAddrPort(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return strings.ToLower(strings.Trim(h, "[]"))
	}
	return strings.ToLower(strings.Trim(s, "[]"))
}

func splitHosts(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if h := stripAddrPort(p); h != "" {
			out = append(out, h)
		}
	}
	return out
}

type registryIndex struct {
	byAddr map[string][]store.Instance
	types  map[string]bool
}

func newRegistryIndex(list []store.Instance) *registryIndex {
	ix := &registryIndex{byAddr: map[string][]store.Instance{}, types: map[string]bool{}}
	for _, it := range list {
		ix.types[normalizeAlertType(it.DBType)] = true
		keys := splitHosts(it.Host)
		keys = append(keys, stripAddrPort(it.DomainName), stripAddrPort(it.Name), stripAddrPort(it.CloudInstanceID))
		for _, d := range it.Domains {
			keys = append(keys, stripAddrPort(d))
		}
		seen := map[string]bool{}
		for _, k := range keys {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			ix.byAddr[k] = append(ix.byAddr[k], it)
		}
	}
	return ix
}

// match 返回序列对应的登记资源；规则 service 为已知类型时要求类型一致。
func (ix *registryIndex) match(labels map[string]string, service string) (*store.Instance, bool) {
	if ix == nil {
		return nil, false
	}
	want := normalizeAlertType(service)
	enforce := want != "" && (strictAlertTypes[want] || ix.types[want])
	for _, lk := range seriesAddrLabels {
		key := stripAddrPort(labels[lk])
		if key == "" {
			continue
		}
		for i := range ix.byAddr[key] {
			it := ix.byAddr[key][i]
			if enforce && normalizeAlertType(it.DBType) != want {
				continue
			}
			return &it, true
		}
	}
	return nil, false
}

func registeredLabels(labels map[string]string, it *store.Instance) map[string]string {
	out := make(map[string]string, len(labels)+3)
	for k, v := range labels {
		out[k] = v
	}
	if it == nil {
		return out
	}
	if hosts := splitHosts(it.Host); len(hosts) > 0 {
		out[LabelRegisteredHost] = hosts[0]
	}
	if n := strings.TrimSpace(it.Name); n != "" {
		out[LabelRegisteredName] = n
	}
	if t := strings.TrimSpace(it.DBType); t != "" {
		out[LabelRegisteredType] = t
	}
	return out
}
