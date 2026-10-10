package cmdb

import (
	"encoding/json"
	"net"
	"sort"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// CIType 配置项类型。
const (
	TypeInstance      = "instance"
	TypeDatabase      = "database"
	TypeRegion        = "region"
	TypeEnvironment   = "environment"
	TypeConnector     = "connector"
	TypeMonitorJob    = "monitor_job"
	TypeMonitorGroup  = "monitor_group"
	TypeHACluster     = "ha_cluster"
	TypeBackupPolicy  = "backup_policy"
	TypeHost          = "host"
)

// CI 统一目录条目。
type CI struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Status    string         `json:"status,omitempty"`
	EnvID     string         `json:"env_id,omitempty"`
	RegionID  string         `json:"region_id,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
	EditPath  string         `json:"edit_path,omitempty"`
}

// Edge 关系边。
type Edge struct {
	SrcType string `json:"src_type"`
	SrcID   string `json:"src_id"`
	RelType string `json:"rel_type"`
	DstType string `json:"dst_type"`
	DstID   string `json:"dst_id"`
	Source  string `json:"source"` // derived | manual | sync
	Remark  string `json:"remark,omitempty"`
}

// Graph 拓扑。
type Graph struct {
	Nodes []CI   `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// ListOpts 目录筛选。
type ListOpts struct {
	Type     string
	TenantID string
	EnvID    string
	RegionID string
	Q        string
	Tag      string
	Limit    int
	Offset   int
}

// Service CMDB 门面服务。
type Service struct {
	Store *store.DB
}

func New(db *store.DB) *Service {
	return &Service{Store: db}
}

func (s *Service) ListCIs(opts ListOpts) ([]CI, error) {
	t := strings.TrimSpace(opts.Type)
	if t == "" {
		t = TypeInstance
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	var all []CI
	var err error
	switch t {
	case TypeInstance:
		all, err = s.listInstances(opts)
	case TypeDatabase:
		all, err = s.listDatabases(opts)
	case TypeRegion:
		all, err = s.listRegions(opts)
	case TypeEnvironment:
		all, err = s.listEnvironments(opts)
	case TypeConnector:
		all, err = s.listConnectors(opts)
	case TypeMonitorJob:
		all, err = s.listMonitorJobs(opts)
	case TypeMonitorGroup:
		all, err = s.listMonitorGroups(opts)
	case TypeHACluster:
		all, err = s.listHAClusters(opts)
	case TypeBackupPolicy:
		all, err = s.listBackupPolicies(opts)
	case TypeHost:
		all, err = s.listHosts(opts)
	default:
		return []CI{}, nil
	}
	if err != nil {
		return nil, err
	}
	all = filterCIs(all, opts)
	if opts.Offset > len(all) {
		return []CI{}, nil
	}
	end := opts.Offset + opts.Limit
	if end > len(all) {
		end = len(all)
	}
	return all[opts.Offset:end], nil
}

func filterCIs(list []CI, opts ListOpts) []CI {
	q := strings.ToLower(strings.TrimSpace(opts.Q))
	tag := strings.TrimSpace(opts.Tag)
	env := strings.TrimSpace(opts.EnvID)
	region := strings.TrimSpace(opts.RegionID)
	out := make([]CI, 0, len(list))
	for _, c := range list {
		if env != "" && c.EnvID != env {
			continue
		}
		if region != "" && c.RegionID != region {
			continue
		}
		if tag != "" {
			ok := false
			for _, t := range c.Tags {
				if t == tag || strings.HasPrefix(t, tag+"=") || strings.Contains(t, tag) {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		if q != "" {
			blob := strings.ToLower(c.Name + " " + c.ID + " " + strings.Join(c.Tags, " "))
			if attrs, _ := json.Marshal(c.Attrs); len(attrs) > 0 {
				blob += " " + strings.ToLower(string(attrs))
			}
			if !strings.Contains(blob, q) {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func (s *Service) GetCI(ciType, id string) (*CI, error) {
	return s.GetCIInTenant(ciType, id, "")
}

func (s *Service) GetCIInTenant(ciType, id, tenantID string) (*CI, error) {
	list, err := s.ListCIs(ListOpts{Type: ciType, TenantID: tenantID, Limit: 5000})
	if err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	for i := range list {
		if list[i].ID == id {
			c := list[i]
			return &c, nil
		}
	}
	// host 特殊
	if ciType == TypeHost && strings.HasPrefix(id, "host:") {
		for i := range list {
			if list[i].ID == id {
				c := list[i]
				return &c, nil
			}
		}
	}
	return nil, nil
}

func (s *Service) listInstances(opts ListOpts) ([]CI, error) {
	list, err := s.Store.ListInstancesInTenant(opts.TenantID, "", opts.Q)
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(list))
	for _, it := range list {
		out = append(out, CI{
			ID: it.ID, Type: TypeInstance, Name: firstNonEmpty(it.Name, it.DomainName, it.Host),
			Status: "active", EnvID: it.EnvID, RegionID: it.RegionID, Tags: it.Tags,
			Attrs: map[string]any{
				"db_type": it.DBType, "host": it.Host, "port": it.Port,
				"domain_name": it.DomainName, "domains": it.Domains, "idc": it.IDC,
				"cloud_vendor": it.CloudVendor, "cloud_instance_id": it.CloudInstanceID,
			},
			UpdatedAt: it.UpdatedAt, EditPath: "/config/instances?db_type=" + it.DBType,
		})
	}
	return out, nil
}

func (s *Service) listDatabases(opts ListOpts) ([]CI, error) {
	list, err := s.Store.ListDatabasesInTenant(opts.TenantID, "", opts.Q)
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(list))
	for _, db := range list {
		out = append(out, CI{
			ID: db.ID, Type: TypeDatabase, Name: db.DBName, Status: "registered",
			Tags: db.Tags,
			Attrs: map[string]any{
				"instance_id": db.InstanceID, "db_name": db.DBName, "domains": db.Domains,
			},
			UpdatedAt: db.UpdatedAt, EditPath: "/config/databases",
		})
	}
	return out, nil
}

func (s *Service) listRegions(_ ListOpts) ([]CI, error) {
	list, err := s.Store.ListRegions()
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(list))
	for _, r := range list {
		out = append(out, CI{
			ID: r.ID, Type: TypeRegion, Name: firstNonEmpty(r.Name, r.Code),
			Attrs: map[string]any{"code": r.Code, "description": r.Description},
			UpdatedAt: r.UpdatedAt, EditPath: "/config/regions",
		})
	}
	return out, nil
}

func (s *Service) listEnvironments(_ ListOpts) ([]CI, error) {
	list, err := s.Store.ListEnvironments()
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(list))
	for _, e := range list {
		out = append(out, CI{
			ID: e.ID, Type: TypeEnvironment, Name: firstNonEmpty(e.Name, e.Code),
			Attrs: map[string]any{"code": e.Code, "description": e.Description},
			UpdatedAt: e.UpdatedAt, EditPath: "/config/environments",
		})
	}
	return out, nil
}

func (s *Service) listConnectors(opts ListOpts) ([]CI, error) {
	list, err := s.Store.ListConnectorsInTenant(opts.TenantID, "")
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(list))
	for _, c := range list {
		out = append(out, CI{
			ID: c.ID, Type: TypeConnector, Name: firstNonEmpty(c.Remark, c.DBZHost, c.ID),
			Attrs: map[string]any{"env": c.Env, "dbz_host": c.DBZHost, "kafka_host": c.KafkaHost},
			UpdatedAt: c.UpdatedAt, EditPath: "/config/connectors",
		})
	}
	_ = opts
	return out, nil
}

func (s *Service) listMonitorJobs(opts ListOpts) ([]CI, error) {
	list, err := s.Store.ListMonitorJobsInTenant(opts.TenantID, opts.Q)
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(list))
	for _, j := range list {
		out = append(out, CI{
			ID: j.ID, Type: TypeMonitorJob, Name: j.Name, Status: j.Status,
			Attrs: map[string]any{"collector": j.Collector, "monitor_type": j.MonitorType, "template": j.Template},
			UpdatedAt: j.UpdatedAt, EditPath: "/config/monitor-targets",
		})
	}
	return out, nil
}

func (s *Service) listMonitorGroups(opts ListOpts) ([]CI, error) {
	jobs, err := s.Store.ListMonitorJobsInTenant(opts.TenantID, "")
	if err != nil {
		return nil, err
	}
	var out []CI
	for _, j := range jobs {
		groups, err := s.Store.ListMonitorTargetGroups(j.ID)
		if err != nil {
			return nil, err
		}
		for _, g := range groups {
			eps := splitEndpoints(g.Endpoints)
			name := g.ID
			if len(eps) > 0 {
				name = eps[0]
			}
			out = append(out, CI{
				ID: g.ID, Type: TypeMonitorGroup, Name: name, Status: "active",
				Attrs: map[string]any{"job_id": g.JobID, "endpoints": eps, "labels": g.Labels},
				UpdatedAt: g.UpdatedAt, EditPath: "/config/monitor-targets",
			})
		}
	}
	if out == nil {
		out = []CI{}
	}
	return out, nil
}

func (s *Service) listHAClusters(opts ListOpts) ([]CI, error) {
	list, err := s.Store.ListHAClustersInTenant(opts.TenantID, "", opts.EnvID, opts.Q)
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(list))
	for _, c := range list {
		out = append(out, CI{
			ID: c.ID, Type: TypeHACluster, Name: firstNonEmpty(c.Name, c.ID), Status: c.MaintStatus,
			EnvID: c.EnvID,
			Attrs: map[string]any{"engine": c.Engine, "mode": c.Mode, "vip": c.VIP, "controller_id": c.ControllerID},
			UpdatedAt: c.UpdatedAt, EditPath: "/ops/ha",
		})
	}
	return out, nil
}

func (s *Service) listBackupPolicies(opts ListOpts) ([]CI, error) {
	list, err := s.Store.ListBackupPoliciesInTenant(opts.TenantID, "")
	if err != nil {
		return nil, err
	}
	out := make([]CI, 0, len(list))
	for _, p := range list {
		status := "disabled"
		if p.Enabled {
			status = "enabled"
		}
		out = append(out, CI{
			ID: p.ID, Type: TypeBackupPolicy, Name: firstNonEmpty(p.Name, p.ID), Status: status,
			Attrs: map[string]any{"instance_id": p.InstanceID, "engine": p.Engine, "method": p.Method, "schedule_expr": p.ScheduleExpr},
			UpdatedAt: p.UpdatedAt, EditPath: "/ops/backup/policies",
		})
	}
	_ = opts
	return out, nil
}

func (s *Service) listHosts(opts ListOpts) ([]CI, error) {
	list, err := s.Store.ListInstancesInTenant(opts.TenantID, "", opts.Q)
	if err != nil {
		return nil, err
	}
	byHost := map[string][]string{}
	hostMeta := map[string]CI{}
	for _, it := range list {
		host := strings.TrimSpace(it.Host)
		if host == "" {
			continue
		}
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		id := "host:" + host
		byHost[id] = append(byHost[id], it.ID)
		if _, ok := hostMeta[id]; !ok {
			hostMeta[id] = CI{
				ID: id, Type: TypeHost, Name: host, Status: "active",
				EnvID: it.EnvID, RegionID: it.RegionID, Tags: it.Tags,
				Attrs: map[string]any{"host": host},
				UpdatedAt: it.UpdatedAt,
			}
		}
	}
	keys := make([]string, 0, len(hostMeta))
	for k := range hostMeta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]CI, 0, len(keys))
	for _, k := range keys {
		c := hostMeta[k]
		c.Attrs["instance_ids"] = byHost[k]
		out = append(out, c)
	}
	return out, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func splitEndpoints(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Audit snapshots helpers —— 脱敏。
func RedactInstance(it store.Instance) store.Instance {
	it.Password = ""
	return it
}

func SnapshotTime() string {
	return time.Now().UTC().Format(time.RFC3339)
}
