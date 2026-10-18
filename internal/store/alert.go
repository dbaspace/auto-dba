package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// 告警规则 / 实例状态
const (
	AlertSeverityP0 = "P0"
	AlertSeverityP1 = "P1"
	AlertSeverityP2 = "P2"
	AlertSeverityP3 = "P3"

	AlertStatusPending  = "pending"
	AlertStatusFiring   = "firing"
	AlertStatusResolved = "resolved"

	AlertHandleUnclaimed    = "unclaimed"
	AlertHandleAcknowledged = "acknowledged"
	AlertHandleProcessing   = "processing"
	AlertHandleSilenced     = "silenced"
	AlertHandleResolved     = "resolved"

	AlertSilenceActive  = "active"
	AlertSilencePending = "pending"
	AlertSilenceExpired = "expired"
)

type AlertRule struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id,omitempty"`
	Name         string            `json:"name"`
	Expr         string            `json:"expr"`
	MetricExpr   string            `json:"metric_expr,omitempty"` // 多档时：无比较符的指标表达式
	Severity     string            `json:"severity"`
	ForDuration  string            `json:"for_duration"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	Cluster      string            `json:"cluster"`
	Scope        AlertScope        `json:"scope"`
	Tiers        []AlertTier       `json:"tiers,omitempty"`
	NotifyPolicy AlertNotifyPolicy `json:"notify_policy"`
	CompiledExpr string            `json:"compiled_expr,omitempty"`
	CompiledAt   string            `json:"compiled_at,omitempty"`
	CompileError string            `json:"compile_error,omitempty"`
	Enabled      bool              `json:"enabled"`
	Health       int               `json:"health"`
	Triggered7d  int               `json:"triggered_7d"`
	NoiseRate    float64           `json:"noise_rate"`
	CreatedBy    string            `json:"created_by"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
}

type Alert struct {
	ID             string            `json:"id"`
	TenantID       string            `json:"tenant_id,omitempty"`
	Fingerprint    string            `json:"fingerprint"`
	RuleID         string            `json:"rule_id"`
	Severity       string            `json:"severity"`
	Title          string            `json:"title"`
	Service        string            `json:"service"`
	Cluster        string            `json:"cluster"`
	Labels         map[string]string `json:"labels"`
	Annotations    map[string]string `json:"annotations"`
	Status         string            `json:"status"`
	Handle         string            `json:"handle"`
	Owner          string            `json:"owner"`
	CurValue       string            `json:"cur_value"`
	Threshold      string            `json:"threshold"`
	AggregateCount int               `json:"aggregate_count"`
	StartedAt      string            `json:"started_at"`
	ResolvedAt     string            `json:"resolved_at"`
	PendingSince   string            `json:"pending_since"`
	LastNotifiedAt  string `json:"last_notified_at,omitempty"`
	NotifyCount     int    `json:"notify_count,omitempty"`
	ResolveNotified bool   `json:"resolve_notified,omitempty"` // 本生命周期是否已外发恢复通知
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type AlertEvent struct {
	ID        string `json:"id"`
	AlertID   string `json:"alert_id"`
	Action    string `json:"action"`
	Detail    string `json:"detail"`
	Operator  string `json:"operator"`
	CreatedAt string `json:"created_at"`
}

type AlertSilence struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id,omitempty"`
	Name      string `json:"name"`
	Matchers  string `json:"matchers"`
	StartsAt  string `json:"starts_at"`
	EndsAt    string `json:"ends_at"`
	Reason    string `json:"reason"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Affected  int    `json:"affected,omitempty"`
}

type AlertFilter struct {
	Tab      string // active|unclaimed|claimed|silenced|history|all
	Severity string
	Service  string
	Keyword  string
	Since    string // RFC3339
	Until    string // RFC3339，仅历史按事件时间使用
	TenantID string
}

type AlertStats struct {
	Active    int    `json:"active"`
	Unclaimed int    `json:"unclaimed"`
	P0        int    `json:"p0"`
	MTTA      string `json:"mtta"`
	MTTR      string `json:"mttr"`
}

func (d *DB) ensureAlertTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tbl_alert_rules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			expr TEXT NOT NULL DEFAULT '',
			metric_expr TEXT NOT NULL DEFAULT '',
			severity TEXT NOT NULL DEFAULT 'P2',
			for_duration TEXT NOT NULL DEFAULT '5m',
			labels TEXT NOT NULL DEFAULT '{}',
			annotations TEXT NOT NULL DEFAULT '{}',
			cluster TEXT NOT NULL DEFAULT '',
			scope_json TEXT NOT NULL DEFAULT '{"mode":"global"}',
			tiers_json TEXT NOT NULL DEFAULT '[]',
			notify_json TEXT NOT NULL DEFAULT '{}',
			compiled_expr TEXT NOT NULL DEFAULT '',
			compiled_at TEXT NOT NULL DEFAULT '',
			compile_error TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			health INTEGER NOT NULL DEFAULT 100,
			triggered_7d INTEGER NOT NULL DEFAULT 0,
			noise_rate DOUBLE PRECISION NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS tbl_alerts (
			id TEXT PRIMARY KEY,
			fingerprint TEXT NOT NULL DEFAULT '',
			rule_id TEXT NOT NULL DEFAULT '',
			severity TEXT NOT NULL DEFAULT 'P2',
			title TEXT NOT NULL DEFAULT '',
			service TEXT NOT NULL DEFAULT '',
			cluster TEXT NOT NULL DEFAULT '',
			labels TEXT NOT NULL DEFAULT '{}',
			annotations TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT 'pending',
			handle TEXT NOT NULL DEFAULT 'unclaimed',
			owner TEXT NOT NULL DEFAULT '',
			cur_value TEXT NOT NULL DEFAULT '',
			threshold TEXT NOT NULL DEFAULT '',
			aggregate_count INTEGER NOT NULL DEFAULT 1,
			started_at TEXT NOT NULL DEFAULT '',
			resolved_at TEXT NOT NULL DEFAULT '',
			pending_since TEXT NOT NULL DEFAULT '',
			last_notified_at TEXT NOT NULL DEFAULT '',
			notify_count INTEGER NOT NULL DEFAULT 0,
			resolve_notified INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_alerts_fp_open ON tbl_alerts(fingerprint) WHERE status != 'resolved'`,
		`CREATE INDEX IF NOT EXISTS idx_alerts_status ON tbl_alerts(status, handle)`,
		`CREATE TABLE IF NOT EXISTS tbl_alert_events (
			id TEXT PRIMARY KEY,
			alert_id TEXT NOT NULL,
			action TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			operator TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_events_alert ON tbl_alert_events(alert_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alert_events_created ON tbl_alert_events(created_at)`,
		`CREATE TABLE IF NOT EXISTS tbl_alert_silences (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			matchers TEXT NOT NULL DEFAULT '',
			starts_at TEXT NOT NULL DEFAULT '',
			ends_at TEXT NOT NULL DEFAULT '',
			reason TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			return err
		}
	}
	for _, ddl := range []string{
		`ALTER TABLE tbl_alert_rules ADD COLUMN IF NOT EXISTS scope_json TEXT NOT NULL DEFAULT '{"mode":"global"}'`,
		`ALTER TABLE tbl_alert_rules ADD COLUMN IF NOT EXISTS compiled_expr TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tbl_alert_rules ADD COLUMN IF NOT EXISTS compiled_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tbl_alert_rules ADD COLUMN IF NOT EXISTS compile_error TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tbl_alert_rules ADD COLUMN IF NOT EXISTS metric_expr TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tbl_alert_rules ADD COLUMN IF NOT EXISTS tiers_json TEXT NOT NULL DEFAULT '[]'`,
		`ALTER TABLE tbl_alert_rules ADD COLUMN IF NOT EXISTS notify_json TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE tbl_alerts ADD COLUMN IF NOT EXISTS last_notified_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tbl_alerts ADD COLUMN IF NOT EXISTS notify_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE tbl_alerts ADD COLUMN IF NOT EXISTS resolve_notified INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := d.Exec(ddl); err != nil {
			return err
		}
	}
	if err := d.ensureAlertNotifyBudgetTable(); err != nil {
		return err
	}
	return d.ensureAlertOpsTables()
}

func ValidAlertSeverity(s string) bool {
	switch s {
	case AlertSeverityP0, AlertSeverityP1, AlertSeverityP2, AlertSeverityP3:
		return true
	}
	return false
}

// AlertFingerprint 对 ruleID + 关键 labels 做稳定哈希。
func AlertFingerprint(ruleID string, labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		if k == "__name__" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(ruleID)
	for _, k := range keys {
		b.WriteByte('|')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

// ParseAlertMatchers 解析 "k=v, k2=v2" 为 map（仅精确匹配）。
func ParseAlertMatchers(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		i := strings.IndexByte(part, '=')
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(part[:i])
		v := strings.TrimSpace(part[i+1:])
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// MatchersMatchLabels 判断静默 matchers 是否全部命中 labels。
func MatchersMatchLabels(matchers, labels map[string]string) bool {
	if len(matchers) == 0 {
		return false
	}
	for k, v := range matchers {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// ParseAlertDuration 解析 5m / 1h / 30s。
func ParseAlertDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 5 * time.Minute, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("持续时间无效: %s", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("持续时间须为正数")
	}
	return d, nil
}

func encodeJSONMap(m map[string]string) string {
	if m == nil {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func decodeJSONMap(s string) map[string]string {
	out := map[string]string{}
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		return map[string]string{}
	}
	return out
}

func nowRFC3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func NormalizeAlertRule(r *AlertRule) error {
	r.Name = strings.TrimSpace(r.Name)
	r.Expr = strings.TrimSpace(r.Expr)
	r.Severity = strings.TrimSpace(r.Severity)
	r.ForDuration = strings.TrimSpace(r.ForDuration)
	r.Cluster = strings.TrimSpace(r.Cluster)
	if r.Name == "" {
		return errors.New("请填写规则名")
	}
	r.MetricExpr = strings.TrimSpace(r.MetricExpr)
	r.Expr = strings.TrimSpace(r.Expr)
	tiers, err := NormalizeAlertTiers(r.Tiers)
	if err != nil {
		return err
	}
	r.Tiers = tiers
	r.NotifyPolicy = NormalizeAlertNotifyPolicy(&r.NotifyPolicy)
	if len(r.Tiers) > 0 {
		if r.MetricExpr == "" {
			r.MetricExpr = r.Expr
		}
		if r.MetricExpr == "" {
			return errors.New("多档阈值规则请填写指标表达式 metric_expr")
		}
		// 多档时 expr/compiled 为无比较符的指标模板；阈值在 Ruler 侧按档判定
		r.Expr = r.MetricExpr
		best := r.Tiers[0]
		for _, t := range r.Tiers {
			if severityRank(t.Severity) > severityRank(best.Severity) {
				best = t
			}
		}
		if !ValidAlertSeverity(r.Severity) || r.Severity == "" {
			r.Severity = best.Severity
		}
		if r.ForDuration == "" {
			r.ForDuration = best.For
		}
	} else if r.Expr == "" {
		return errors.New("请填写 PromQL 表达式")
	}
	if !ValidAlertSeverity(r.Severity) {
		r.Severity = AlertSeverityP2
	}
	if r.ForDuration == "" {
		r.ForDuration = "5m"
	}
	if _, err := ParseAlertDuration(r.ForDuration); err != nil {
		return err
	}
	if r.Labels == nil {
		r.Labels = map[string]string{}
	}
	if r.Annotations == nil {
		r.Annotations = map[string]string{}
	}
	if r.Health <= 0 {
		r.Health = 100
	}
	return nil
}

func scanAlertRule(sc interface{ Scan(dest ...any) error }) (*AlertRule, error) {
	var r AlertRule
	var labels, ann, scopeJSON, tiersJSON, notifyJSON string
	var enabled int
	if err := sc.Scan(&r.ID, &r.Name, &r.Expr, &r.MetricExpr, &r.Severity, &r.ForDuration, &labels, &ann, &r.Cluster,
		&scopeJSON, &tiersJSON, &notifyJSON, &r.CompiledExpr, &r.CompiledAt, &r.CompileError,
		&enabled, &r.Health, &r.Triggered7d, &r.NoiseRate, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	r.Enabled = enabled == 1
	r.Labels = decodeJSONMap(labels)
	r.Annotations = decodeJSONMap(ann)
	r.Scope = decodeAlertScope(scopeJSON)
	r.Tiers = decodeAlertTiers(tiersJSON)
	r.NotifyPolicy = decodeAlertNotifyPolicy(notifyJSON)
	return &r, nil
}

const alertRuleCols = `id, name, expr, metric_expr, severity, for_duration, labels, annotations, cluster, scope_json, tiers_json, notify_json, compiled_expr, compiled_at, compile_error, enabled, health, triggered_7d, noise_rate, created_by, created_at, updated_at`

func (d *DB) ListAlertRules() ([]AlertRule, error) {
	return d.ListAlertRulesInTenant("")
}

func (d *DB) ListAlertRulesInTenant(tenantID string) ([]AlertRule, error) {
	q := `SELECT ` + alertRuleCols + ` FROM tbl_alert_rules WHERE 1=1`
	args := []any{}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tid)
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		r, err := scanAlertRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (d *DB) ListEnabledAlertRules() ([]AlertRule, error) {
	return d.ListEnabledAlertRulesInTenant("")
}

func (d *DB) ListEnabledAlertRulesInTenant(tenantID string) ([]AlertRule, error) {
	q := `SELECT ` + alertRuleCols + ` FROM tbl_alert_rules WHERE enabled=1`
	args := []any{}
	if tid := strings.TrimSpace(tenantID); tid != "" {
		q += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tid)
	}
	q += ` ORDER BY name`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		r, err := scanAlertRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (d *DB) GetAlertRule(id string) (*AlertRule, error) {
	row := d.QueryRow(`SELECT `+alertRuleCols+` FROM tbl_alert_rules WHERE id=?`, id)
	r, err := scanAlertRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

func (d *DB) DeleteAlertRuleInTenant(id, tenantID string) error {
	r, err := d.GetAlertRuleInTenant(id, tenantID)
	if err != nil || r == nil {
		return err
	}
	return d.DeleteAlertRule(id)
}

func (d *DB) CreateAlertRule(r *AlertRule) error {
	if err := NormalizeAlertRule(r); err != nil {
		return err
	}
	if strings.TrimSpace(r.Scope.Mode) == "" {
		r.Scope = DefaultAlertScope()
	}
	if err := d.CompileAlertRuleScope(r); err != nil && r.Scope.Mode != AlertScopeModeGlobal {
		return err
	}
	now := nowRFC3339(time.Now())
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.CreatedAt, r.UpdatedAt = now, now
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_alert_rules (`+alertRuleCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Name, r.Expr, r.MetricExpr, r.Severity, r.ForDuration, encodeJSONMap(r.Labels), encodeJSONMap(r.Annotations),
		r.Cluster, encodeAlertScope(r.Scope), encodeAlertTiers(r.Tiers), encodeAlertNotifyPolicy(r.NotifyPolicy),
		r.CompiledExpr, r.CompiledAt, r.CompileError,
		en, r.Health, r.Triggered7d, r.NoiseRate, r.CreatedBy, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_alert_rules", r.ID, r.TenantID)
}

func (d *DB) SaveAlertRule(r *AlertRule) error {
	if err := NormalizeAlertRule(r); err != nil {
		return err
	}
	if strings.TrimSpace(r.Scope.Mode) == "" {
		r.Scope = DefaultAlertScope()
	}
	if err := d.CompileAlertRuleScope(r); err != nil && r.Scope.Mode != AlertScopeModeGlobal {
		return err
	}
	r.UpdatedAt = nowRFC3339(time.Now())
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`UPDATE tbl_alert_rules SET name=?, expr=?, metric_expr=?, severity=?, for_duration=?, labels=?, annotations=?,
		cluster=?, scope_json=?, tiers_json=?, notify_json=?, compiled_expr=?, compiled_at=?, compile_error=?,
		enabled=?, health=?, triggered_7d=?, noise_rate=?, updated_at=? WHERE id=?`,
		r.Name, r.Expr, r.MetricExpr, r.Severity, r.ForDuration, encodeJSONMap(r.Labels), encodeJSONMap(r.Annotations),
		r.Cluster, encodeAlertScope(r.Scope), encodeAlertTiers(r.Tiers), encodeAlertNotifyPolicy(r.NotifyPolicy),
		r.CompiledExpr, r.CompiledAt, r.CompileError,
		en, r.Health, r.Triggered7d, r.NoiseRate, r.UpdatedAt, r.ID)
	return err
}

func (d *DB) DeleteAlertRule(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_alert_rules WHERE id=?`, id)
	return err
}

func scanAlert(sc interface{ Scan(dest ...any) error }) (*Alert, error) {
	var a Alert
	var labels, ann string
	var resolveNotified int
	if err := sc.Scan(&a.ID, &a.Fingerprint, &a.RuleID, &a.Severity, &a.Title, &a.Service, &a.Cluster,
		&labels, &ann, &a.Status, &a.Handle, &a.Owner, &a.CurValue, &a.Threshold, &a.AggregateCount,
		&a.StartedAt, &a.ResolvedAt, &a.PendingSince, &a.LastNotifiedAt, &a.NotifyCount, &resolveNotified,
		&a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.ResolveNotified = resolveNotified != 0
	a.Labels = decodeJSONMap(labels)
	a.Annotations = decodeJSONMap(ann)
	return &a, nil
}

const alertCols = `id, fingerprint, rule_id, severity, title, service, cluster, labels, annotations, status, handle, owner, cur_value, threshold, aggregate_count, started_at, resolved_at, pending_since, last_notified_at, notify_count, resolve_notified, created_at, updated_at`

func (d *DB) GetAlert(id string) (*Alert, error) {
	row := d.QueryRow(`SELECT `+alertCols+` FROM tbl_alerts WHERE id=?`, id)
	a, err := scanAlert(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (d *DB) GetOpenAlertByFingerprint(fp string) (*Alert, error) {
	row := d.QueryRow(`SELECT `+alertCols+` FROM tbl_alerts WHERE fingerprint=? AND status != ?`, fp, AlertStatusResolved)
	a, err := scanAlert(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

// ListAlertsByFingerprint 同指纹历史（含已恢复），按开始时间倒序。
func (d *DB) ListAlertsByFingerprint(fp string, limit int) ([]Alert, error) {
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	rows, err := d.Query(
		`SELECT `+alertCols+` FROM tbl_alerts WHERE fingerprint=? ORDER BY started_at DESC LIMIT ?`,
		fp, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (d *DB) ListAlerts(f AlertFilter) ([]Alert, error) {
	q := `SELECT ` + alertCols + ` FROM tbl_alerts WHERE 1=1`
	args := []any{}
	q, args = appendTenantFilter(q, args, f.TenantID)
	q += ` ORDER BY started_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	kw := strings.ToLower(strings.TrimSpace(f.Keyword))
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		if !alertInFilter(*a, f, kw) {
			continue
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func alertInFilter(a Alert, f AlertFilter, kw string) bool {
	switch f.Tab {
	case "", "active":
		if a.Status == AlertStatusResolved {
			return false
		}
	case "unclaimed":
		if a.Status == AlertStatusResolved || a.Handle != AlertHandleUnclaimed {
			return false
		}
	case "claimed":
		if a.Status == AlertStatusResolved || (a.Handle != AlertHandleAcknowledged && a.Handle != AlertHandleProcessing) {
			return false
		}
	case "silenced":
		if a.Handle != AlertHandleSilenced {
			return false
		}
	case "history":
		if a.Status != AlertStatusResolved {
			return false
		}
	case "all":
	}
	if f.Severity != "" && a.Severity != f.Severity {
		return false
	}
	if f.Service != "" && a.Service != f.Service {
		return false
	}
	if f.Since != "" && a.StartedAt != "" && a.StartedAt < f.Since {
		return false
	}
	if kw != "" {
		hay := strings.ToLower(strings.Join([]string{a.Title, a.Service, a.Cluster, a.ID, a.Fingerprint, a.Owner}, " "))
		if !strings.Contains(hay, kw) {
			return false
		}
	}
	return true
}

func (d *DB) AlertStats() AlertStats {
	return d.AlertStatsInTenant("")
}

func (d *DB) AlertStatsInTenant(tenantID string) AlertStats {
	list, err := d.ListAlerts(AlertFilter{Tab: "all", TenantID: tenantID})
	if err != nil {
		return AlertStats{}
	}
	var st AlertStats
	for _, a := range list {
		if a.Status != AlertStatusResolved {
			st.Active++
			if a.Handle == AlertHandleUnclaimed {
				st.Unclaimed++
			}
			if a.Severity == AlertSeverityP0 {
				st.P0++
			}
		}
	}
	st.MTTA = "—"
	st.MTTR = "—"
	return st
}

func (d *DB) UpsertAlert(a *Alert) error {
	now := nowRFC3339(time.Now())
	if a.ID == "" {
		a.ID = uuid.NewString()
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Labels == nil {
		a.Labels = map[string]string{}
	}
	if a.Annotations == nil {
		a.Annotations = map[string]string{}
	}
	if a.AggregateCount <= 0 {
		a.AggregateCount = 1
	}
	rn := 0
	if a.ResolveNotified {
		rn = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_alerts (`+alertCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			severity=EXCLUDED.severity, title=EXCLUDED.title, service=EXCLUDED.service, cluster=EXCLUDED.cluster,
			labels=EXCLUDED.labels, annotations=EXCLUDED.annotations, status=EXCLUDED.status, handle=EXCLUDED.handle,
			owner=EXCLUDED.owner, cur_value=EXCLUDED.cur_value, threshold=EXCLUDED.threshold,
			aggregate_count=EXCLUDED.aggregate_count, started_at=EXCLUDED.started_at, resolved_at=EXCLUDED.resolved_at,
			pending_since=EXCLUDED.pending_since, last_notified_at=EXCLUDED.last_notified_at, notify_count=EXCLUDED.notify_count,
			resolve_notified=EXCLUDED.resolve_notified, updated_at=EXCLUDED.updated_at`,
		a.ID, a.Fingerprint, a.RuleID, a.Severity, a.Title, a.Service, a.Cluster,
		encodeJSONMap(a.Labels), encodeJSONMap(a.Annotations), a.Status, a.Handle, a.Owner,
		a.CurValue, a.Threshold, a.AggregateCount, a.StartedAt, a.ResolvedAt, a.PendingSince,
		a.LastNotifiedAt, a.NotifyCount, rn, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(a.TenantID) == "" && strings.TrimSpace(a.RuleID) != "" {
		if r, e := d.GetAlertRule(a.RuleID); e == nil && r != nil {
			a.TenantID = r.TenantID
		}
	}
	if strings.TrimSpace(a.TenantID) != "" {
		return d.AssignTenantID("tbl_alerts", a.ID, a.TenantID)
	}
	return nil
}

func (d *DB) SaveAlert(a *Alert) error {
	a.UpdatedAt = nowRFC3339(time.Now())
	rn := 0
	if a.ResolveNotified {
		rn = 1
	}
	_, err := d.Exec(`UPDATE tbl_alerts SET severity=?, title=?, service=?, cluster=?, labels=?, annotations=?,
		status=?, handle=?, owner=?, cur_value=?, threshold=?, aggregate_count=?, started_at=?, resolved_at=?,
		pending_since=?, last_notified_at=?, notify_count=?, resolve_notified=?, updated_at=? WHERE id=?`,
		a.Severity, a.Title, a.Service, a.Cluster, encodeJSONMap(a.Labels), encodeJSONMap(a.Annotations),
		a.Status, a.Handle, a.Owner, a.CurValue, a.Threshold, a.AggregateCount, a.StartedAt, a.ResolvedAt,
		a.PendingSince, a.LastNotifiedAt, a.NotifyCount, rn, a.UpdatedAt, a.ID)
	return err
}

func (d *DB) AddAlertEvent(e *AlertEvent) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt == "" {
		e.CreatedAt = nowRFC3339(time.Now())
	}
	_, err := d.Exec(`INSERT INTO tbl_alert_events (id, alert_id, action, detail, operator, created_at) VALUES (?,?,?,?,?,?)`,
		e.ID, e.AlertID, e.Action, e.Detail, e.Operator, e.CreatedAt)
	return err
}

func (d *DB) ListAlertEvents(alertID string) ([]AlertEvent, error) {
	rows, err := d.Query(`SELECT id, alert_id, action, detail, operator, created_at FROM tbl_alert_events WHERE alert_id=? ORDER BY created_at DESC`, alertID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertEvent
	for rows.Next() {
		var e AlertEvent
		if err := rows.Scan(&e.ID, &e.AlertID, &e.Action, &e.Detail, &e.Operator, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func RefreshSilenceStatus(s *AlertSilence, now time.Time) {
	st, err1 := time.Parse(time.RFC3339, s.StartsAt)
	et, err2 := time.Parse(time.RFC3339, s.EndsAt)
	if err1 != nil || err2 != nil {
		return
	}
	switch {
	case now.Before(st):
		s.Status = AlertSilencePending
	case now.After(et):
		s.Status = AlertSilenceExpired
	default:
		s.Status = AlertSilenceActive
	}
}

func scanSilence(sc interface{ Scan(dest ...any) error }) (*AlertSilence, error) {
	var s AlertSilence
	if err := sc.Scan(&s.ID, &s.Name, &s.Matchers, &s.StartsAt, &s.EndsAt, &s.Reason, &s.Status, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

const silenceCols = `id, name, matchers, starts_at, ends_at, reason, status, created_by, created_at, updated_at`

func (d *DB) ListAlertSilences() ([]AlertSilence, error) {
	return d.ListAlertSilencesInTenant("")
}

func (d *DB) ListAlertSilencesInTenant(tenantID string) ([]AlertSilence, error) {
	q := `SELECT ` + silenceCols + ` FROM tbl_alert_silences WHERE 1=1`
	args := []any{}
	q, args = appendTenantFilter(q, args, tenantID)
	q += ` ORDER BY starts_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	var out []AlertSilence
	for rows.Next() {
		s, err := scanSilence(rows)
		if err != nil {
			return nil, err
		}
		RefreshSilenceStatus(s, now)
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (d *DB) ListActiveAlertSilences(now time.Time) ([]AlertSilence, error) {
	list, err := d.ListAlertSilences()
	if err != nil {
		return nil, err
	}
	var out []AlertSilence
	for _, s := range list {
		RefreshSilenceStatus(&s, now)
		if s.Status == AlertSilenceActive {
			out = append(out, s)
		}
	}
	return out, nil
}

func (d *DB) GetAlertSilence(id string) (*AlertSilence, error) {
	row := d.QueryRow(`SELECT `+silenceCols+` FROM tbl_alert_silences WHERE id=?`, id)
	s, err := scanSilence(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err == nil {
		RefreshSilenceStatus(s, time.Now())
	}
	return s, err
}

func (d *DB) CreateAlertSilence(s *AlertSilence) error {
	s.Name = strings.TrimSpace(s.Name)
	s.Matchers = strings.TrimSpace(s.Matchers)
	s.Reason = strings.TrimSpace(s.Reason)
	if s.Name == "" {
		return errors.New("请填写静默名称")
	}
	if s.Matchers == "" {
		return errors.New("请填写匹配条件")
	}
	if _, err := time.Parse(time.RFC3339, s.StartsAt); err != nil {
		return errors.New("开始时间无效")
	}
	if _, err := time.Parse(time.RFC3339, s.EndsAt); err != nil {
		return errors.New("结束时间无效")
	}
	now := nowRFC3339(time.Now())
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	s.CreatedAt, s.UpdatedAt = now, now
	RefreshSilenceStatus(s, time.Now())
	_, err := d.Exec(`INSERT INTO tbl_alert_silences (`+silenceCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Name, s.Matchers, s.StartsAt, s.EndsAt, s.Reason, s.Status, s.CreatedBy, s.CreatedAt, s.UpdatedAt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(s.TenantID) != "" {
		return d.AssignTenantID("tbl_alert_silences", s.ID, s.TenantID)
	}
	return nil
}

func (d *DB) CancelAlertSilence(id string) error {
	s, err := d.GetAlertSilence(id)
	if err != nil {
		return err
	}
	if s == nil {
		return errors.New("静默不存在")
	}
	now := time.Now().UTC()
	s.EndsAt = now.Format(time.RFC3339)
	s.Status = AlertSilenceExpired
	s.UpdatedAt = nowRFC3339(now)
	_, err = d.Exec(`UPDATE tbl_alert_silences SET ends_at=?, status=?, updated_at=? WHERE id=?`,
		s.EndsAt, s.Status, s.UpdatedAt, id)
	return err
}

// CountSilenceAffected 统计当前活跃告警中被该静默匹配的数量。
func (d *DB) CountSilenceAffected(matchers string) (int, error) {
	return d.CountSilenceAffectedInTenant("", matchers)
}

func (d *DB) CountSilenceAffectedInTenant(tenantID, matchers string) (int, error) {
	m := ParseAlertMatchers(matchers)
	list, err := d.ListAlerts(AlertFilter{Tab: "active", TenantID: tenantID})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range list {
		if MatchersMatchLabels(m, a.Labels) {
			n++
		}
	}
	return n, nil
}
