package cmdb

import (
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

// DiscoverDBCandidate live 库发现候选（由 handler 填 Registered）。
type DiscoverDBCandidate struct {
	DBName     string `json:"db_name"`
	Registered bool   `json:"registered"`
}

// MonitorCandidate 从监控 Target Group 产出的候选实例。
type MonitorCandidate struct {
	Endpoint  string            `json:"endpoint"`
	Host      string            `json:"host"`
	Port      int               `json:"port"`
	Domain    string            `json:"domain,omitempty"`
	IDC       string            `json:"idc,omitempty"`
	JobID     string            `json:"job_id"`
	JobName   string            `json:"job_name"`
	GroupID   string            `json:"group_id"`
	Labels    map[string]string `json:"labels,omitempty"`
	MatchedID string            `json:"matched_instance_id,omitempty"`
	TenantID  string            `json:"tenant_id,omitempty"`
}

// ClusterCandidate 从 SR/部署/HA 节点反推的未登记主机或实例候选。
type ClusterCandidate struct {
	Source     string `json:"source"` // sr_cluster | db_deploy | ha | agent
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name,omitempty"`
	TenantID   string `json:"tenant_id,omitempty"`
	Engine     string `json:"engine,omitempty"`
	Host       string `json:"host"`
	Port       int    `json:"port,omitempty"`
	Role       string `json:"role,omitempty"`
	MatchedID  string `json:"matched_instance_id,omitempty"`
	DedupKey   string `json:"dedup_key"`
}

// DiscoverFromMonitor 扫描监控登记，产出候选（不入库）。
func (s *Service) DiscoverFromMonitor() ([]MonitorCandidate, error) {
	return s.DiscoverFromMonitorInTenant("")
}

func (s *Service) DiscoverFromMonitorInTenant(tenantID string) ([]MonitorCandidate, error) {
	jobs, err := s.Store.ListMonitorJobs("")
	if err != nil {
		return nil, err
	}
	insts, err := s.Store.ListInstancesInTenant(tenantID, "", "")
	if err != nil {
		return nil, err
	}
	byHostPort := map[string]string{}
	byDomain := map[string]string{}
	for _, it := range insts {
		key := fmt.Sprintf("%s:%d", strings.TrimSpace(it.Host), it.Port)
		byHostPort[key] = it.ID
		if it.DomainName != "" {
			byDomain[it.DomainName] = it.ID
		}
		for _, d := range it.Domains {
			byDomain[d] = it.ID
		}
	}
	var out []MonitorCandidate
	seen := map[string]bool{}
	for _, j := range jobs {
		groups, err := s.Store.ListMonitorTargetGroups(j.ID)
		if err != nil {
			return nil, err
		}
		for _, g := range groups {
			for _, ep := range splitEndpoints(g.Endpoints) {
				host, port := parseEndpoint(ep)
				if host == "" {
					continue
				}
				uk := fmt.Sprintf("%s:%d", host, port)
				if seen[uk] {
					continue
				}
				seen[uk] = true
				c := MonitorCandidate{
					Endpoint: ep, Host: host, Port: port,
					Domain: g.Labels["domain"], IDC: firstNonEmpty(g.Labels["idc"], j.Labels["idc"]),
					JobID: j.ID, JobName: j.Name, GroupID: g.ID, Labels: g.Labels,
					TenantID: tenantID,
				}
				if id := byHostPort[uk]; id != "" {
					c.MatchedID = id
				} else if c.Domain != "" {
					c.MatchedID = byDomain[c.Domain]
				}
				out = append(out, c)
			}
		}
	}
	if out == nil {
		out = []MonitorCandidate{}
	}
	return out, nil
}

// DiscoverFromClusters 从 SR/部署/HA 节点反推未登记候选。
func (s *Service) DiscoverFromClusters(tenantID string) ([]ClusterCandidate, error) {
	insts, err := s.Store.ListInstancesInTenant(tenantID, "", "")
	if err != nil {
		return nil, err
	}
	byKey := map[string]string{}
	for _, it := range insts {
		byKey[dedupKey(it.TenantID, it.Host, it.Port, it.DBType)] = it.ID
	}

	var out []ClusterCandidate
	seen := map[string]bool{}
	add := func(c ClusterCandidate) {
		c.DedupKey = dedupKey(c.TenantID, c.Host, c.Port, c.Engine)
		if c.Host == "" || seen[c.DedupKey] {
			return
		}
		seen[c.DedupKey] = true
		if id := byKey[c.DedupKey]; id != "" {
			c.MatchedID = id
		}
		out = append(out, c)
	}

	srs, _ := s.Store.ListSrClustersInTenant(tenantID, "")
	for _, cl := range srs {
		nodes, _ := s.Store.ListSrNodes(cl.ID)
		for _, n := range nodes {
			add(ClusterCandidate{
				Source: TypeSrCluster, SourceID: cl.ID, SourceName: cl.Name,
				TenantID: cl.TenantID, Engine: "starrocks", Host: n.Host, Port: n.HTTPPort, Role: n.Component,
			})
		}
	}
	deploys, _ := s.Store.ListDbDeployClustersInTenant(tenantID, "")
	for _, cl := range deploys {
		nodes, _ := s.Store.ListDbDeployNodes(cl.ID)
		for _, n := range nodes {
			add(ClusterCandidate{
				Source: TypeDbDeployCluster, SourceID: cl.ID, SourceName: cl.Name,
				TenantID: cl.TenantID, Engine: cl.Engine, Host: n.Host, Port: n.Port, Role: n.Role,
			})
		}
	}
	has, _ := s.Store.ListHAClusters("", "", "")
	for _, c := range has {
		if tenantID != "" && c.TenantID != "" && c.TenantID != tenantID {
			continue
		}
		members, _ := s.Store.ListHAMembers(c.ID)
		for _, m := range members {
			if m.InstanceID != "" {
				if it, _ := s.Store.GetInstance(m.InstanceID); it != nil {
					add(ClusterCandidate{
						Source: TypeHACluster, SourceID: c.ID, SourceName: c.Name,
						TenantID: firstNonEmpty(c.TenantID, it.TenantID), Engine: it.DBType,
						Host: it.Host, Port: it.Port, Role: m.Role, MatchedID: it.ID,
					})
					continue
				}
			}
		}
	}
	// Agent 登记：有 host_instance_id 则视为已匹配主机实例；否则用 BaseURL host 作候选
	agents, _ := s.Store.ListDbDeployAgentsInTenant(tenantID)
	for _, a := range agents {
		host := hostFromBaseURL(a.BaseURL)
		c := ClusterCandidate{
			Source: "agent", SourceID: a.ID, SourceName: a.Name,
			TenantID: tenantID, Host: host, Role: "db_deploy_agent",
		}
		if a.HostInstID != "" {
			c.MatchedID = a.HostInstID
			if it, _ := s.Store.GetInstance(a.HostInstID); it != nil {
				c.Host = firstNonEmpty(it.Host, host)
				c.TenantID = firstNonEmpty(it.TenantID, tenantID)
			}
		}
		add(c)
	}
	srAgents, _ := s.Store.ListSrAgentsInTenant(tenantID)
	for _, a := range srAgents {
		host := hostFromBaseURL(a.BaseURL)
		c := ClusterCandidate{
			Source: "agent", SourceID: a.ID, SourceName: a.Name,
			TenantID: tenantID, Host: host, Role: "sr_agent",
		}
		if a.HostInstID != "" {
			c.MatchedID = a.HostInstID
			if it, _ := s.Store.GetInstance(a.HostInstID); it != nil {
				c.Host = firstNonEmpty(it.Host, host)
				c.TenantID = firstNonEmpty(it.TenantID, tenantID)
			}
		}
		add(c)
	}
	if out == nil {
		out = []ClusterCandidate{}
	}
	return out, nil
}

func hostFromBaseURL(base string) string {
	base = strings.TrimSpace(base)
	base = strings.TrimPrefix(base, "http://")
	base = strings.TrimPrefix(base, "https://")
	if i := strings.IndexByte(base, '/'); i >= 0 {
		base = base[:i]
	}
	host, _ := parseEndpoint(base)
	return host
}

func dedupKey(tenantID, host string, port int, engine string) string {
	return strings.TrimSpace(tenantID) + "|" + strings.ToLower(strings.TrimSpace(host)) + "|" +
		fmt.Sprintf("%d", port) + "|" + strings.ToLower(strings.TrimSpace(engine))
}

func parseEndpoint(ep string) (host string, port int) {
	ep = strings.TrimSpace(ep)
	ep = strings.TrimPrefix(ep, "http://")
	ep = strings.TrimPrefix(ep, "https://")
	if i := strings.IndexByte(ep, '/'); i >= 0 {
		ep = ep[:i]
	}
	host = ep
	port = 0
	if i := strings.LastIndexByte(ep, ':'); i > 0 {
		host = ep[:i]
		fmt.Sscanf(ep[i+1:], "%d", &port)
	}
	return host, port
}

// RegisterDatabases 批量登记库（复用 store）。
func (s *Service) RegisterDatabases(instanceID string, dbNames []string, actor string) (created, skipped []string, err error) {
	tpl := &store.Database{InstanceID: instanceID, Approvers: []string{}, Tags: []string{}, Visibility: "global"}
	created, skipped, err = s.Store.CreateDatabases(tpl, dbNames)
	if err != nil {
		return
	}
	for _, name := range created {
		_ = s.Store.AppendCmdbAudit(actor, "create", TypeDatabase, instanceID+":"+name, "api", nil, map[string]any{"db_name": name, "instance_id": instanceID})
	}
	return
}
