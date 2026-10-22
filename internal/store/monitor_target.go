package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type MonitorJob struct {
	ID             string               `json:"id"`
	TenantID       string               `json:"tenant_id,omitempty"`
	Name           string               `json:"name"`
	Template       string               `json:"template"`
	Collector      string               `json:"collector"`
	Scheme         string               `json:"scheme"`
	Status         string               `json:"status"`
	MonitorType    string               `json:"monitor_type"`
	MetricsPath    string               `json:"metrics_path"`
	ScrapeInterval string               `json:"scrape_interval"`
	ScrapeTimeout  string               `json:"scrape_timeout"`
	Labels         map[string]string    `json:"labels"`
	Description    string               `json:"description"`
	TargetGroups   []MonitorTargetGroup `json:"target_groups,omitempty"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
}

type MonitorTargetGroup struct {
	ID          string            `json:"id"`
	JobID       string            `json:"job_id"`
	Endpoints   string            `json:"endpoints"`
	Labels      map[string]string `json:"labels"`
	Description string            `json:"description"`
	SortOrder   int               `json:"sort_order"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

type MonitorPreviewItem struct {
	TaskName  string            `json:"task_name"`
	Env       string            `json:"env"`
	Domain    string            `json:"domain"`
	Version   string            `json:"version,omitempty"`
	Endpoints []string          `json:"endpoints"`
	Labels    map[string]string `json:"labels,omitempty"`
	LoginURL  string            `json:"login_url,omitempty"`
}

const monitorJobCols = `id, name, template, collector, scheme, status, monitor_type, metrics_path, scrape_interval, scrape_timeout, labels_json, description, created_at, updated_at`
const monitorTGCols = `id, job_id, endpoints, labels_json, description, sort_order, created_at, updated_at`

func encodeLabels(m map[string]string) string {
	if m == nil {
		m = map[string]string{}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func decodeLabels(s string) map[string]string {
	out := map[string]string{}
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = map[string]string{}
	}
	return out
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

func scanMonitorJob(scanner interface {
	Scan(dest ...any) error
}) (*MonitorJob, error) {
	var it MonitorJob
	var labelsJSON string
	if err := scanner.Scan(
		&it.ID, &it.Name, &it.Template, &it.Collector, &it.Scheme, &it.Status, &it.MonitorType,
		&it.MetricsPath, &it.ScrapeInterval, &it.ScrapeTimeout, &labelsJSON, &it.Description,
		&it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return nil, err
	}
	it.Labels = decodeLabels(labelsJSON)
	return &it, nil
}

func scanMonitorTG(scanner interface {
	Scan(dest ...any) error
}) (*MonitorTargetGroup, error) {
	var it MonitorTargetGroup
	var labelsJSON string
	if err := scanner.Scan(&it.ID, &it.JobID, &it.Endpoints, &labelsJSON, &it.Description, &it.SortOrder, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	it.Labels = decodeLabels(labelsJSON)
	return &it, nil
}

func (d *DB) ListMonitorJobs(keyword string) ([]MonitorJob, error) {
	return d.ListMonitorJobsInTenant("", keyword)
}

func (d *DB) ListMonitorJobsInTenant(tenantID, keyword string) ([]MonitorJob, error) {
	q := `SELECT ` + monitorJobCols + ` FROM tbl_monitor_job WHERE 1=1`
	var args []any
	q, args = appendTenantFilter(q, args, tenantID)
	if kw := strings.TrimSpace(keyword); kw != "" {
		q += ` AND (name ILIKE ? OR collector ILIKE ? OR description ILIKE ?)`
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	q += ` ORDER BY updated_at DESC, created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MonitorJob
	for rows.Next() {
		it, err := scanMonitorJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) GetMonitorJob(id string) (*MonitorJob, error) {
	it, err := scanMonitorJob(d.QueryRow(`SELECT `+monitorJobCols+` FROM tbl_monitor_job WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	groups, err := d.ListMonitorTargetGroups(id)
	if err != nil {
		return nil, err
	}
	it.TargetGroups = groups
	return it, nil
}

func (d *DB) GetMonitorJobInTenant(id, tenantID string) (*MonitorJob, error) {
	it, err := d.GetMonitorJob(id)
	return ownedInTenant(d, it, err, "tbl_monitor_job", id, tenantID)
}

func (d *DB) ListMonitorTargetGroups(jobID string) ([]MonitorTargetGroup, error) {
	rows, err := d.Query(`SELECT `+monitorTGCols+` FROM tbl_monitor_target_group WHERE job_id=? ORDER BY sort_order ASC, created_at ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MonitorTargetGroup
	for rows.Next() {
		it, err := scanMonitorTG(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) CreateMonitorJob(it *MonitorJob) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.CreatedAt = now
	it.UpdatedAt = now
	normalizeMonitorJob(it)
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		d.rewrite(`INSERT INTO tbl_monitor_job(`+monitorJobCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`),
		it.ID, it.Name, it.Template, it.Collector, it.Scheme, it.Status, it.MonitorType,
		it.MetricsPath, it.ScrapeInterval, it.ScrapeTimeout, encodeLabels(it.Labels), it.Description,
		it.CreatedAt, it.UpdatedAt,
	)
	if err != nil {
		return err
	}
	for i := range it.TargetGroups {
		g := &it.TargetGroups[i]
		g.JobID = it.ID
		g.SortOrder = i
		if err := d.insertMonitorTGTx(tx, g); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if strings.TrimSpace(it.TenantID) == "" {
		it.TenantID = DefaultTenantID
	}
	return d.AssignTenantID("tbl_monitor_job", it.ID, it.TenantID)
}

func (d *DB) insertMonitorTGTx(tx *sql.Tx, g *MonitorTargetGroup) error {
	if g.ID == "" {
		g.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if g.CreatedAt == "" {
		g.CreatedAt = now
	}
	g.UpdatedAt = now
	if g.Labels == nil {
		g.Labels = map[string]string{}
	}
	_, err := tx.Exec(
		d.rewrite(`INSERT INTO tbl_monitor_target_group(`+monitorTGCols+`) VALUES(?,?,?,?,?,?,?,?)`),
		g.ID, g.JobID, strings.TrimSpace(g.Endpoints), encodeLabels(g.Labels), strings.TrimSpace(g.Description),
		g.SortOrder, g.CreatedAt, g.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateMonitorJob(it *MonitorJob) error {
	normalizeMonitorJob(it)
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		d.rewrite(`UPDATE tbl_monitor_job SET name=?, template=?, collector=?, scheme=?, status=?, monitor_type=?, metrics_path=?, scrape_interval=?, scrape_timeout=?, labels_json=?, description=?, updated_at=? WHERE id=?`),
		it.Name, it.Template, it.Collector, it.Scheme, it.Status, it.MonitorType,
		it.MetricsPath, it.ScrapeInterval, it.ScrapeTimeout, encodeLabels(it.Labels), it.Description,
		it.UpdatedAt, it.ID,
	)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_monitor_target_group WHERE job_id=?`), it.ID); err != nil {
		return err
	}
	for i := range it.TargetGroups {
		g := &it.TargetGroups[i]
		g.JobID = it.ID
		g.SortOrder = i
		g.CreatedAt = ""
		if err := d.insertMonitorTGTx(tx, g); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) DeleteMonitorJob(id string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_monitor_target_group WHERE job_id=?`), id); err != nil {
		return err
	}
	if _, err := tx.Exec(d.rewrite(`DELETE FROM tbl_monitor_job WHERE id=?`), id); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizeMonitorJob(it *MonitorJob) {
	it.Name = strings.TrimSpace(it.Name)
	it.Template = strings.TrimSpace(it.Template)
	if it.Template == "" {
		it.Template = "直连类型监控模板"
	}
	it.Collector = strings.TrimSpace(it.Collector)
	it.Scheme = strings.TrimSpace(it.Scheme)
	if it.Scheme == "" {
		it.Scheme = "http"
	}
	it.Status = strings.TrimSpace(it.Status)
	if it.Status == "" {
		it.Status = "active"
	}
	it.MonitorType = strings.TrimSpace(it.MonitorType)
	if it.MonitorType == "" {
		it.MonitorType = "direct"
	}
	it.MetricsPath = strings.TrimSpace(it.MetricsPath)
	if it.MetricsPath == "" {
		it.MetricsPath = "/metrics"
	}
	it.ScrapeInterval = strings.TrimSpace(it.ScrapeInterval)
	if it.ScrapeInterval == "" {
		it.ScrapeInterval = "1m"
	}
	it.ScrapeTimeout = strings.TrimSpace(it.ScrapeTimeout)
	if it.ScrapeTimeout == "" {
		it.ScrapeTimeout = "10s"
	}
	it.Description = strings.TrimSpace(it.Description)
	if it.Labels == nil {
		it.Labels = map[string]string{}
	}
}

// starRocksPreviewRole 区分监控目标是 FE 还是 BE。端口优先于 app 标签；
// 只看 app/component/role，避免 idc=beijing 这类值里的 “be” 被当成 BE。
func starRocksPreviewRole(endpoint string, labels map[string]string) string {
	switch monitorEndpointPort(endpoint) {
	case 8030, 8031, 9010, 9020:
		return "fe"
	case 8040, 8041, 8060, 9050, 9060, 9070:
		return "be"
	}
	app := monitorRoleLabel(labels)
	switch {
	case appLooksBE(app):
		return "be"
	case appLooksFE(app):
		return "fe"
	default:
		return ""
	}
}

func monitorRoleLabel(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	for _, k := range []string{"app", "component", "role", "group", "job"} {
		if v := strings.TrimSpace(labels[k]); v != "" {
			return v
		}
	}
	return ""
}

func appLooksBE(app string) bool {
	app = strings.ToLower(strings.TrimSpace(app))
	if app == "" {
		return false
	}
	// be-StarRocks / be / starrocks-be；fe-StarRocks 不能算 BE
	return strings.Contains(app, "be-") || strings.HasPrefix(app, "be") || strings.Contains(app, "-be") || app == "be"
}

func appLooksFE(app string) bool {
	app = strings.ToLower(strings.TrimSpace(app))
	if app == "" || appLooksBE(app) {
		return false
	}
	return strings.Contains(app, "fe")
}

func monitorEndpointPort(endpoint string) int {
	ep := strings.TrimSpace(endpoint)
	if ep == "" {
		return 0
	}
	_, port, err := net.SplitHostPort(ep)
	if err != nil {
		i := strings.LastIndex(ep, ":")
		if i < 0 {
			return 0
		}
		port = ep[i+1:]
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return n
}

func starRocksPreviewEndpoints(role string, endpoints []string, labels map[string]string) []string {
	var out []string
	for _, ep := range endpoints {
		if starRocksPreviewRole(ep, labels) == role {
			out = append(out, ep)
		}
	}
	return out
}

func pickLabel(labels ...map[string]string) (env, domain, loginURL string) {
	for _, m := range labels {
		if m == nil {
			continue
		}
		if env == "" {
			for _, k := range []string{"idc", "env", "environment"} {
				if v := strings.TrimSpace(m[k]); v != "" {
					env = v
					break
				}
			}
		}
		if domain == "" {
			if v := strings.TrimSpace(m["domain"]); v != "" {
				domain = v
			}
		}
		if loginURL == "" {
			if v := strings.TrimSpace(m["login_url"]); v != "" {
				loginURL = v
			}
		}
	}
	return
}

func mergeLabels(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if k == "" || v == "" {
				continue
			}
			out[k] = v
		}
	}
	return out
}

// pickVersionLabel 从监控标签取版本（version / sr_version / starrocks_version）。
func pickVersionLabel(labels ...map[string]string) string {
	for _, m := range labels {
		if m == nil {
			continue
		}
		for _, k := range []string{"version", "sr_version", "starrocks_version", "sr_ver"} {
			if v := strings.TrimSpace(m[k]); v != "" {
				return v
			}
		}
	}
	return ""
}

// PreviewMonitorTargets filters registered jobs/groups for FE/DS config forms.
func (d *DB) PreviewMonitorTargets(kind string) ([]MonitorPreviewItem, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	jobs, err := d.ListMonitorJobs("")
	if err != nil {
		return nil, err
	}
	var out []MonitorPreviewItem
	for _, job := range jobs {
		groups, err := d.ListMonitorTargetGroups(job.ID)
		if err != nil {
			return nil, err
		}
		nameLower := strings.ToLower(job.Name)
		switch kind {
		case "starrocks-fe", "fe", "starrocks-be", "be":
			if !strings.Contains(nameLower, "starrocks") {
				continue
			}
			want := "fe"
			if kind == "starrocks-be" || kind == "be" {
				want = "be"
			}
			for _, g := range groups {
				merged := mergeLabels(job.Labels, g.Labels)
				eps := starRocksPreviewEndpoints(want, splitEndpoints(g.Endpoints), merged)
				if len(eps) == 0 {
					continue
				}
				env, domain, _ := pickLabel(merged)
				if env == "" {
					env = job.Collector
				}
				out = append(out, MonitorPreviewItem{
					TaskName:  job.Name,
					Env:       env,
					Domain:    domain,
					Version:   pickVersionLabel(merged),
					Endpoints: eps,
					Labels:    merged,
				})
			}
		case "dolphinscheduler", "ds":
			if !strings.Contains(nameLower, "dolphinscheduler") && !strings.Contains(nameLower, "dolphin") {
				continue
			}
			env, _, loginURL := pickLabel(job.Labels)
			if env == "" {
				env = job.Collector
			}
			var eps []string
			for _, g := range groups {
				e2, _, login2 := pickLabel(g.Labels)
				if env == "" {
					env = e2
				}
				if loginURL == "" {
					loginURL = login2
				}
				eps = append(eps, splitEndpoints(g.Endpoints)...)
			}
			if loginURL == "" && len(eps) > 0 {
				scheme := job.Scheme
				if scheme == "" {
					scheme = "http"
				}
				loginURL = scheme + "://" + eps[0] + "/dolphinscheduler"
			}
			out = append(out, MonitorPreviewItem{
				TaskName:  job.Name,
				Env:       env,
				Endpoints: eps,
				LoginURL:  loginURL,
				Labels:    job.Labels,
			})
		default:
			return nil, errors.New("unsupported preview kind")
		}
	}
	return out, nil
}
