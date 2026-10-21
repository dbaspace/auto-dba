package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/promclient"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

const (
	sourcePromBase  = "awr_prometheus_base_url"
	sourcePromToken = "awr_prometheus_auth_token"
	maxSeries       = 50
	maxTableRows    = 50
	sqlTimeout      = 20 * time.Second
)

// ParamSpec 模板允许的参数。
type ParamSpec struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// DataTemplate 查询数据源模板。地址不写死在 query 里。
type DataTemplate struct {
	ID        string      `json:"id"`
	Kind      string      `json:"kind"` // prometheus | platform | sql
	Title     string      `json:"title"`
	Engine    string      `json:"engine,omitempty"`
	SourceRef string      `json:"source_ref"`
	Query     string      `json:"query"`
	Params    []ParamSpec `json:"params"`
	Mode      string      `json:"mode"`
	Range     bool        `json:"range,omitempty"` // true 时走 query_range
}

// DataTemplates 首批目录。prometheus 基址来自全局参数，开发环境示例为 query.universe。
func DataTemplates() []DataTemplate {
	return []DataTemplate{
		{
			ID: "prom.pg.connections", Kind: "prometheus", Title: "PostgreSQL 连接数", Engine: "postgres",
			SourceRef: sourcePromBase, Mode: "read",
			Query:  `sum(pg_stat_activity_count{instance=~"{{instance}}"})`,
			Params: []ParamSpec{{Name: "instance", Type: "string", Required: true}},
		},
		{
			ID: "prom.up", Kind: "prometheus", Title: "目标存活", Engine: "all",
			SourceRef: sourcePromBase, Mode: "read",
			Query:  `up{job=~"{{job}}"}`,
			Params: []ParamSpec{{Name: "job", Type: "string", Required: true}},
		},
		{
			ID: "prom.sr.be_cpu", Kind: "prometheus", Title: "StarRocks/Doris BE CPU", Engine: "starrocks",
			SourceRef: sourcePromBase, Mode: "read", Range: true,
			Query:  `100 * (1 - sum by (instance) (rate(starrocks_be_cpu{mode="idle",instance=~"{{host}}.*"}[5m])) / clamp_min(sum by (instance) (rate(starrocks_be_cpu{instance=~"{{host}}.*"}[5m])), 1e-9))`,
			Params: []ParamSpec{{Name: "host", Type: "string", Required: true}, {Name: "range_hours", Type: "int"}},
		},
		{
			ID: "platform.instances", Kind: "platform", Title: "实例目录", Engine: "all",
			SourceRef: "store.instances", Mode: "read", Query: "list_instances",
			Params: []ParamSpec{{Name: "db_type", Type: "string"}},
		},
		{
			ID: "platform.alerts", Kind: "platform", Title: "活跃告警", Engine: "all",
			SourceRef: "store.alerts", Mode: "read", Query: "list_alerts",
			Params: []ParamSpec{{Name: "limit", Type: "int"}},
		},
		{
			ID: "platform.backup_runs", Kind: "platform", Title: "成功备份记录", Engine: "all",
			SourceRef: "store.backup_runs", Mode: "read", Query: "list_backup_runs",
			Params: []ParamSpec{{Name: "limit", Type: "int"}},
		},
		{
			ID: "sql.pg.activity", Kind: "sql", Title: "PostgreSQL 活动会话", Engine: "postgres",
			SourceRef: "instance", Mode: "read",
			Query: `SELECT pid, usename, state, left(query, 200) AS query FROM pg_stat_activity WHERE state IS NOT NULL LIMIT {{limit}}`,
			Params: []ParamSpec{
				{Name: "instance_id", Type: "string", Required: true},
				{Name: "database", Type: "string", Required: true},
				{Name: "limit", Type: "int"},
			},
		},
	}
}

func templateCatalog() string {
	var parts []string
	for _, t := range DataTemplates() {
		parts = append(parts, t.ID+"("+t.Title+")")
	}
	return strings.Join(parts, "、")
}

func findTemplate(id string) (*DataTemplate, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("template_id 必填")
	}
	for _, t := range DataTemplates() {
		if t.ID == id {
			cp := t
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("未登记的数据源模板: %s", id)
}

func renderTemplate(t *DataTemplate, params map[string]string) (string, error) {
	allowed := map[string]ParamSpec{}
	for _, p := range t.Params {
		allowed[p.Name] = p
	}
	for k := range params {
		if _, ok := allowed[k]; !ok {
			return "", fmt.Errorf("参数 %s 不在模板 %s 允许列表", k, t.ID)
		}
	}
	for _, p := range t.Params {
		if p.Required && strings.TrimSpace(params[p.Name]) == "" {
			return "", fmt.Errorf("模板 %s 缺少参数 %s", t.ID, p.Name)
		}
	}
	out := t.Query
	for name, val := range params {
		val = strings.TrimSpace(val)
		if strings.ContainsAny(val, "{}'\"`;") {
			return "", fmt.Errorf("参数 %s 含非法字符", name)
		}
		out = strings.ReplaceAll(out, "{{"+name+"}}", val)
	}
	if strings.Contains(out, "{{") {
		return "", fmt.Errorf("模板 %s 占位符未替换", t.ID)
	}
	return out, nil
}

func (s *Service) runTemplate(ctx context.Context, tenant string, args map[string]any) (string, int, error) {
	if reason := DetectDanger(fmt.Sprint(args)); reason != "" {
		return "", 0, fmt.Errorf("%s", reason)
	}
	id, _ := args["template_id"].(string)
	t, err := findTemplate(id)
	if err != nil {
		return "", 0, err
	}
	params := stringParams(args["params"])
	if t.Range {
		if err = validatePromHost(params["host"]); err != nil {
			return "", 0, err
		}
		params["host"] = promHostRegex(params["host"])
	}
	rendered, err := renderTemplate(t, params)
	if err != nil {
		return "", 0, err
	}
	var body string
	var rows int
	switch t.Kind {
	case "prometheus":
		if t.Range {
			body, rows, err = s.queryPromRange(ctx, rendered, rangeHours(params["range_hours"]))
		} else {
			body, rows, err = s.queryProm(ctx, rendered)
		}
	case "platform":
		body, rows, err = s.queryPlatform(ctx, tenant, t.Query, params)
	case "sql":
		body, rows, err = s.querySQL(ctx, tenant, rendered, params)
	default:
		err = fmt.Errorf("未知数据源类型 %s", t.Kind)
	}
	if err != nil {
		return "", 0, err
	}
	body = Redact(body)
	return fmt.Sprintf("source=%s kind=%s\n%s", t.ID, t.Kind, body), rows, nil
}

func stringParams(v any) map[string]string {
	out := map[string]string{}
	switch m := v.(type) {
	case map[string]any:
		for k, val := range m {
			out[k] = strings.TrimSpace(fmt.Sprint(val))
		}
	case map[string]string:
		for k, val := range m {
			out[k] = strings.TrimSpace(val)
		}
	}
	return out
}

func (s *Service) queryProm(ctx context.Context, promql string) (string, int, error) {
	if s == nil || s.Store == nil {
		return "", 0, fmt.Errorf("未配置 Prometheus 地址")
	}
	base, _ := s.Store.GetGlobalArg(sourcePromBase)
	base = strings.TrimSpace(base)
	if base == "" {
		return "", 0, fmt.Errorf("未配置 Prometheus 地址（awr_prometheus_base_url）")
	}
	token, _ := s.Store.GetGlobalArg(sourcePromToken)
	cli := promclient.New(promclient.Config{BaseURL: base, AuthToken: token, Timeout: sqlTimeout})
	if cli == nil {
		return "", 0, fmt.Errorf("Prometheus 地址无效")
	}
	samples, err := cli.Query(ctx, promql, time.Now())
	if err != nil {
		return "", 0, err
	}
	if len(samples) > maxSeries {
		samples = samples[:maxSeries]
	}
	raw, _ := json.Marshal(samples)
	return string(raw), len(samples), nil
}

func (s *Service) queryPromRange(ctx context.Context, promql string, hours int) (string, int, error) {
	if s == nil || s.Store == nil {
		return "", 0, fmt.Errorf("未配置 Prometheus 地址")
	}
	base, _ := s.Store.GetGlobalArg(sourcePromBase)
	base = strings.TrimSpace(base)
	if base == "" {
		return "", 0, fmt.Errorf("未配置 Prometheus 地址（awr_prometheus_base_url）")
	}
	if hours <= 0 {
		hours = 1
	}
	if hours > 6 {
		hours = 6
	}
	token, _ := s.Store.GetGlobalArg(sourcePromToken)
	cli := promclient.New(promclient.Config{BaseURL: base, AuthToken: token, Timeout: sqlTimeout})
	if cli == nil {
		return "", 0, fmt.Errorf("Prometheus 地址无效")
	}
	end := time.Now()
	start := end.Add(-time.Duration(hours) * time.Hour)
	samples, err := cli.QueryRange(ctx, promql, start, end, time.Minute)
	if err != nil {
		return "", 0, err
	}
	if len(samples) > maxSeries {
		samples = samples[:maxSeries]
	}
	return summarizeRange(samples), len(samples), nil
}

func rangeHours(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 1
	}
	n := 0
	fmt.Sscanf(raw, "%d", &n)
	if n <= 0 {
		return 1
	}
	if n > 6 {
		return 6
	}
	return n
}

var promHostRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`)

func validatePromHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("host 必填")
	}
	if !promHostRE.MatchString(host) {
		return fmt.Errorf("host 含非法字符")
	}
	return nil
}

func promHostRegex(host string) string {
	return strings.ReplaceAll(host, ".", "[.]")
}

func summarizeRange(samples []promclient.Sample) string {
	if len(samples) == 0 {
		return "Prometheus 范围查询没有返回序列"
	}
	var b strings.Builder
	for _, s := range samples {
		inst := s.Metric["instance"]
		if inst == "" {
			inst = "(无 instance 标签)"
		}
		vals := sampleValues(s)
		if len(vals) == 0 {
			fmt.Fprintf(&b, "- %s 无数值\n", inst)
			continue
		}
		min, max, sum := vals[0], vals[0], 0.0
		for _, v := range vals {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
			sum += v
		}
		fmt.Fprintf(&b, "- %s min=%.1f avg=%.1f max=%.1f n=%d\n", inst, min, sum/float64(len(vals)), max, len(vals))
	}
	return b.String()
}

func sampleValues(s promclient.Sample) []float64 {
	var out []float64
	if len(s.Value) >= 2 {
		if v, ok := promFloat(s.Value[1]); ok {
			out = append(out, v)
		}
	}
	for _, pt := range s.Values {
		if len(pt) >= 2 {
			if v, ok := promFloat(pt[1]); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

func promFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		return f, err == nil
	}
}

func (s *Service) queryPlatform(ctx context.Context, tenant, query string, params map[string]string) (string, int, error) {
	_ = ctx
	if s == nil || s.Store == nil {
		return "", 0, fmt.Errorf("平台目录不可用")
	}
	limit := 20
	if params["limit"] != "" {
		fmt.Sscanf(params["limit"], "%d", &limit)
	}
	if limit <= 0 || limit > maxTableRows {
		limit = 20
	}
	switch query {
	case "list_instances":
		list, err := s.Store.ListInstancesInTenant(tenant, params["db_type"], "")
		if err != nil {
			return "", 0, err
		}
		if len(list) > limit {
			list = list[:limit]
		}
		type row struct {
			ID, Name, Host, DBType string
			Port                   int
		}
		out := make([]row, 0, len(list))
		for _, it := range list {
			out = append(out, row{ID: it.ID, Name: it.Name, Host: it.Host, DBType: it.DBType, Port: it.Port})
		}
		raw, _ := json.Marshal(out)
		return string(raw), len(out), nil
	case "list_alerts":
		list, err := s.Store.ListAlerts(store.AlertFilter{Tab: "active", TenantID: tenant})
		if err != nil {
			return "", 0, err
		}
		if len(list) > limit {
			list = list[:limit]
		}
		type row struct {
			ID, Title, Severity, Status string
		}
		out := make([]row, 0, len(list))
		for _, a := range list {
			out = append(out, row{ID: a.ID, Title: a.Title, Severity: a.Severity, Status: a.Status})
		}
		raw, _ := json.Marshal(out)
		return string(raw), len(out), nil
	case "list_backup_runs":
		list, err := s.Store.ListBackupRunsInTenant(tenant, "", "", store.BackupRunSuccess, limit)
		if err != nil {
			return "", 0, err
		}
		type row struct {
			ID, Engine, Method, Status string
		}
		out := make([]row, 0, len(list))
		for _, r := range list {
			out = append(out, row{ID: r.ID, Engine: r.Engine, Method: r.Method, Status: r.Status})
		}
		raw, _ := json.Marshal(out)
		return string(raw), len(out), nil
	default:
		return "", 0, fmt.Errorf("未知平台查询 %s", query)
	}
}

func (s *Service) querySQL(ctx context.Context, tenant, rendered string, params map[string]string) (string, int, error) {
	sqlText, err := CheckCopilotSQL(rendered)
	if err != nil {
		return "", 0, err
	}
	if s == nil || s.Store == nil {
		return "", 0, fmt.Errorf("无法执行 SQL 模板")
	}
	inst, err := s.Store.GetInstance(params["instance_id"])
	if err != nil || inst == nil {
		return "", 0, fmt.Errorf("实例不存在")
	}
	if tenant != "" && inst.TenantID != "" && inst.TenantID != tenant {
		return "", 0, fmt.Errorf("实例不属于当前租户")
	}
	if strings.TrimSpace(s.Secret) == "" || strings.TrimSpace(inst.Password) == "" {
		return "", 0, fmt.Errorf("实例密码不可用，已拒绝执行")
	}
	pass, err := secret.Decrypt(s.Secret, inst.Password)
	if err != nil {
		return "", 0, fmt.Errorf("解密实例密码失败")
	}
	limit := s.Store.QueryDefaultLimit()
	if limit <= 0 || limit > maxTableRows {
		limit = maxTableRows
	}
	qctx, cancel := context.WithTimeout(dbprobe.WithReadOnlySession(ctx), sqlTimeout)
	defer cancel()
	res, err := dbprobe.ExecQuery(qctx, inst, pass, params["database"], sqlText, limit)
	if err != nil {
		return "", 0, err
	}
	n := len(res.Rows)
	if n > maxTableRows {
		res.Rows = res.Rows[:maxTableRows]
		n = maxTableRows
	}
	raw, _ := json.Marshal(map[string]any{"columns": res.Columns, "rows": res.Rows, "truncated": res.Truncated || len(res.Rows) >= maxTableRows})
	return string(raw), n, nil
}
