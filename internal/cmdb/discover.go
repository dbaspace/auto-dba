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
}

// DiscoverFromMonitor 扫描监控登记，产出候选（不入库）。
func (s *Service) DiscoverFromMonitor() ([]MonitorCandidate, error) {
	jobs, err := s.Store.ListMonitorJobs("")
	if err != nil {
		return nil, err
	}
	insts, err := s.Store.ListInstances("", "")
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
