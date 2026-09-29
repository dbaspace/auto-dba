package dbmonitor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"auto-dba/internal/store"
)

// ClusterItem StarRocks 等基于监控目标注册的集群。
// IDC 字段存「环境管理」中的环境 code（合规），非注册标签原文。
type ClusterItem struct {
	ID          string   `json:"id"`
	IDC         string   `json:"idc"` // 环境 code
	EnvName     string   `json:"env_name,omitempty"`
	Domain      string   `json:"domain"`
	FEEndpoints []string `json:"fe_endpoints"`
	BEEndpoints []string `json:"be_endpoints"`
	JobName     string   `json:"job_name,omitempty"`
}

// ListClusters 从监控目标注册聚合集群；环境必须能映射到「环境管理」中的 code，否则丢弃（不合规）。
func (s *Service) ListClusters(engine string) ([]ClusterItem, error) {
	engine = NormalizeEngine(engine)
	if engine != "starrocks" {
		return []ClusterItem{}, nil
	}
	if s.Store == nil {
		return nil, fmt.Errorf("store not configured")
	}
	envs, err := s.Store.ListEnvironments()
	if err != nil {
		return nil, err
	}
	envByCode, envByName := buildEnvLookups(envs)

	jobs, err := s.Store.ListMonitorJobs("")
	if err != nil {
		return nil, err
	}
	type agg struct {
		idc, envName, domain, job string
		fe, be                    map[string]struct{}
	}
	byKey := map[string]*agg{}
	for _, job := range jobs {
		nameLower := strings.ToLower(job.Name)
		if !strings.Contains(nameLower, "starrocks") && !strings.Contains(nameLower, "doris") {
			continue
		}
		groups, err := s.Store.ListMonitorTargetGroups(job.ID)
		if err != nil {
			return nil, err
		}
		for _, g := range groups {
			rawEnv, domain := clusterIDCDomain(g.Labels, job.Labels)
			if domain == "" {
				continue
			}
			if rawEnv == "" {
				rawEnv = strings.TrimSpace(job.Collector)
			}
			envCode, envName, ok := resolveEnvCode(rawEnv, envByCode, envByName)
			if !ok {
				// 注册标签无法对应环境管理 → 不合规，跳过
				continue
			}
			key := envCode + "|" + domain
			a := byKey[key]
			if a == nil {
				a = &agg{
					idc: envCode, envName: envName, domain: domain, job: job.Name,
					fe: map[string]struct{}{}, be: map[string]struct{}{},
				}
				byKey[key] = a
			}
			eps := splitEndpointList(g.Endpoints)
			app := strings.ToLower(strings.TrimSpace(g.Labels["app"]))
			for _, ep := range eps {
				switch classifyStarRocksEndpoint(ep, app, g.Labels) {
				case "fe":
					a.fe[ep] = struct{}{}
				case "be":
					a.be[ep] = struct{}{}
				}
			}
		}
	}
	out := make([]ClusterItem, 0, len(byKey))
	for _, a := range byKey {
		item := ClusterItem{
			ID:          a.idc + "|" + a.domain,
			IDC:         a.idc,
			EnvName:     a.envName,
			Domain:      a.domain,
			JobName:     a.job,
			FEEndpoints: mapKeysSorted(a.fe),
			BEEndpoints: mapKeysSorted(a.be),
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IDC != out[j].IDC {
			return out[i].IDC < out[j].IDC
		}
		return out[i].Domain < out[j].Domain
	})
	return out, nil
}

func buildEnvLookups(envs []store.Environment) (byCode, byName map[string]store.Environment) {
	byCode = map[string]store.Environment{}
	byName = map[string]store.Environment{}
	for _, e := range envs {
		code := strings.ToLower(strings.TrimSpace(e.Code))
		name := strings.ToLower(strings.TrimSpace(e.Name))
		if code != "" {
			byCode[code] = e
		}
		if name != "" {
			byName[name] = e
		}
	}
	return
}

// resolveEnvCode：注册侧 idc/env 标签必须命中环境管理的 code 或 name，返回规范 code。
func resolveEnvCode(raw string, byCode, byName map[string]store.Environment) (code, name string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	key := strings.ToLower(raw)
	if e, hit := byCode[key]; hit {
		return strings.TrimSpace(e.Code), strings.TrimSpace(e.Name), true
	}
	if e, hit := byName[key]; hit {
		return strings.TrimSpace(e.Code), strings.TrimSpace(e.Name), true
	}
	return "", "", false
}

func clusterIDCDomain(groupLabels, jobLabels map[string]string) (idc, domain string) {
	for _, m := range []map[string]string{groupLabels, jobLabels} {
		if m == nil {
			continue
		}
		if idc == "" {
			for _, k := range []string{"idc", "env", "environment"} {
				if v := strings.TrimSpace(m[k]); v != "" {
					idc = v
					break
				}
			}
		}
		if domain == "" {
			if v := strings.TrimSpace(m["domain"]); v != "" {
				domain = v
			}
		}
	}
	return
}

// classifyStarRocksEndpoint 区分 FE/BE。
// 现场常把 BE 也打成 app=fe-StarRocks，因此端口优先于 app 标签。
func classifyStarRocksEndpoint(endpoint, app string, labels map[string]string) string {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return ""
	}
	if portLooksBE(ep) {
		return "be"
	}
	if portLooksFE(ep) {
		return "fe"
	}
	app = strings.ToLower(strings.TrimSpace(app))
	switch {
	case appLooksBE(app):
		return "be"
	case appLooksFE(app):
		return "fe"
	case labelsHasRole(labels, "be"):
		return "be"
	case labelsHasRole(labels, "fe"):
		return "fe"
	default:
		return ""
	}
}

func appLooksFE(app string) bool {
	app = strings.ToLower(strings.TrimSpace(app))
	if app == "" {
		return false
	}
	if appLooksBE(app) {
		return false
	}
	return strings.Contains(app, "fe")
}

func appLooksBE(app string) bool {
	app = strings.ToLower(strings.TrimSpace(app))
	if app == "" {
		return false
	}
	// be-StarRocks / be / starrocks-be；避免把 fe-StarRocks 误判为 be
	if strings.Contains(app, "be-") || strings.HasPrefix(app, "be") || strings.Contains(app, "-be") {
		return true
	}
	return app == "be"
}

func labelsHasRole(labels map[string]string, role string) bool {
	role = strings.ToLower(strings.TrimSpace(role))
	if labels == nil || role == "" {
		return false
	}
	for _, k := range []string{"app", "component", "role", "group", "job"} {
		v := strings.ToLower(strings.TrimSpace(labels[k]))
		if v == "" {
			continue
		}
		if role == "be" && appLooksBE(v) {
			return true
		}
		if role == "fe" && appLooksFE(v) {
			return true
		}
	}
	return false
}

func endpointPort(endpoint string) int {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return 0
	}
	// [ipv6]:port
	if strings.HasPrefix(ep, "[") {
		if i := strings.LastIndex(ep, "]:"); i >= 0 {
			p, err := strconv.Atoi(ep[i+2:])
			if err == nil {
				return p
			}
		}
		return 0
	}
	if i := strings.LastIndexByte(ep, ':'); i >= 0 {
		p, err := strconv.Atoi(ep[i+1:])
		if err == nil {
			return p
		}
	}
	return 0
}

func portLooksFE(endpoint string) bool {
	switch endpointPort(endpoint) {
	case 8030, 8031, 9010, 9020:
		return true
	default:
		return false
	}
}

func portLooksBE(endpoint string) bool {
	switch endpointPort(endpoint) {
	case 8040, 8041, 8060, 9050, 9060, 9070:
		return true
	default:
		return false
	}
}

func splitEndpointList(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';' || r == ' '
	})
	var out []string
	seen := map[string]struct{}{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func mapKeysSorted(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
