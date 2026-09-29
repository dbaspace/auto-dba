package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ---------- Metric ----------

type SqlInsightMetricRow struct {
	ID           string
	MetricKey    string
	Name         string
	Description  string
	Source       string
	ExprType     string
	BuiltinField string
	Unit         string
	Stat         string
	ScopeHost    string
	ScopePort    int
	Enabled      bool
	System       bool
	CreatedBy    string
	UpdatedBy    string
	CreatedAt    time.Time
	UpdatedAt    *time.Time
}

func (d *DB) InsertSqlInsightMetric(ctx context.Context, r *SqlInsightMetricRow) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_sql_insight_metrics (
  id, metric_key, name, description, source, expr_type, builtin_field, unit, stat,
  scope_host, scope_port, enabled, system, created_by, updated_by, created_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())`),
		r.ID, r.MetricKey, r.Name, r.Description, r.Source, r.ExprType, r.BuiltinField, r.Unit, r.Stat,
		r.ScopeHost, r.ScopePort, r.Enabled, r.System, r.CreatedBy, r.UpdatedBy)
	return err
}

func (d *DB) UpdateSqlInsightMetric(ctx context.Context, r *SqlInsightMetricRow) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_sql_insight_metrics SET
  name=?, description=?, source=?, expr_type=?, builtin_field=?, unit=?, stat=?,
  scope_host=?, scope_port=?, enabled=?, updated_by=?, updated_at=NOW()
WHERE id=? AND system=FALSE`),
		r.Name, r.Description, r.Source, r.ExprType, r.BuiltinField, r.Unit, r.Stat,
		r.ScopeHost, r.ScopePort, r.Enabled, r.UpdatedBy, r.ID)
	return err
}

func (d *DB) DeleteSqlInsightMetric(ctx context.Context, id string) error {
	res, err := d.db.ExecContext(ctx, d.rewrite(`DELETE FROM tbl_sql_insight_metrics WHERE id=? AND system=FALSE`), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("指标不存在或为系统预置不可删")
	}
	return nil
}

func (d *DB) GetSqlInsightMetricByID(ctx context.Context, id string) (*SqlInsightMetricRow, error) {
	return d.scanSqlInsightMetric(ctx, "SELECT "+siMetricCols+" FROM tbl_sql_insight_metrics WHERE id=?", id)
}

func (d *DB) GetSqlInsightMetricByKey(ctx context.Context, key string) (*SqlInsightMetricRow, error) {
	return d.scanSqlInsightMetric(ctx, "SELECT "+siMetricCols+" FROM tbl_sql_insight_metrics WHERE metric_key=?", key)
}

const siMetricCols = `id, metric_key, name, description, source, expr_type, builtin_field, unit, stat,
  scope_host, scope_port, enabled, system, created_by, updated_by, created_at, updated_at`

func (d *DB) scanSqlInsightMetric(ctx context.Context, q string, args ...any) (*SqlInsightMetricRow, error) {
	r := &SqlInsightMetricRow{}
	var updated sql.NullTime
	err := d.db.QueryRowContext(ctx, d.rewrite(q), args...).Scan(
		&r.ID, &r.MetricKey, &r.Name, &r.Description, &r.Source, &r.ExprType, &r.BuiltinField, &r.Unit, &r.Stat,
		&r.ScopeHost, &r.ScopePort, &r.Enabled, &r.System, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &updated)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("指标不存在")
	}
	if err != nil {
		return nil, err
	}
	if updated.Valid {
		t := updated.Time
		r.UpdatedAt = &t
	}
	return r, nil
}

func (d *DB) ListSqlInsightMetrics(ctx context.Context, source string, enabledOnly bool, offset, limit int) ([]*SqlInsightMetricRow, int, error) {
	var conds []string
	var args []any
	if strings.TrimSpace(source) != "" {
		conds = append(conds, "source=?")
		args = append(args, source)
	}
	if enabledOnly {
		conds = append(conds, "enabled=TRUE")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := d.db.QueryRowContext(ctx, d.rewrite("SELECT COUNT(*) FROM tbl_sql_insight_metrics "+where), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	q := fmt.Sprintf(`SELECT %s FROM tbl_sql_insight_metrics %s ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		siMetricCols, where)
	args = append(args, limit, offset)
	rows, err := d.db.QueryContext(ctx, d.rewrite(q), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*SqlInsightMetricRow
	for rows.Next() {
		r := &SqlInsightMetricRow{}
		var updated sql.NullTime
		if err := rows.Scan(
			&r.ID, &r.MetricKey, &r.Name, &r.Description, &r.Source, &r.ExprType, &r.BuiltinField, &r.Unit, &r.Stat,
			&r.ScopeHost, &r.ScopePort, &r.Enabled, &r.System, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &updated); err != nil {
			return nil, 0, err
		}
		if updated.Valid {
			t := updated.Time
			r.UpdatedAt = &t
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (d *DB) ListSqlInsightEnabledMetrics(ctx context.Context) ([]*SqlInsightMetricRow, error) {
	list, _, err := d.ListSqlInsightMetrics(ctx, "", true, 0, 1000)
	return list, err
}

// ---------- Rule ----------

type SqlInsightRuleRow struct {
	ID             string
	Name           string
	Description    string
	MetricKey      string
	RuleKind       string
	Direction      string
	WarnThreshold  *float64
	CritThreshold  *float64
	BaselineWindow string
	ZScoreK        float64
	Enabled        bool
	System         bool
	CreatedBy      string
	UpdatedBy      string
	CreatedAt      time.Time
	UpdatedAt      *time.Time
}

const siRuleCols = `id, name, description, metric_key, rule_kind, direction, warn_threshold, crit_threshold,
  baseline_window, zscore_k, enabled, system, created_by, updated_by, created_at, updated_at`

func scanSqlInsightRule(sc interface {
	Scan(dest ...any) error
}) (*SqlInsightRuleRow, error) {
	r := &SqlInsightRuleRow{}
	var warn, crit sql.NullFloat64
	var updated sql.NullTime
	if err := sc.Scan(
		&r.ID, &r.Name, &r.Description, &r.MetricKey, &r.RuleKind, &r.Direction, &warn, &crit,
		&r.BaselineWindow, &r.ZScoreK, &r.Enabled, &r.System, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &updated); err != nil {
		return nil, err
	}
	if warn.Valid {
		v := warn.Float64
		r.WarnThreshold = &v
	}
	if crit.Valid {
		v := crit.Float64
		r.CritThreshold = &v
	}
	if updated.Valid {
		t := updated.Time
		r.UpdatedAt = &t
	}
	return r, nil
}

func (d *DB) InsertSqlInsightRule(ctx context.Context, r *SqlInsightRuleRow) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_sql_insight_rules (
  id, name, description, metric_key, rule_kind, direction, warn_threshold, crit_threshold,
  baseline_window, zscore_k, enabled, system, created_by, updated_by, created_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())`),
		r.ID, r.Name, r.Description, r.MetricKey, r.RuleKind, r.Direction, r.WarnThreshold, r.CritThreshold,
		r.BaselineWindow, r.ZScoreK, r.Enabled, r.System, r.CreatedBy, r.UpdatedBy)
	return err
}

func (d *DB) UpdateSqlInsightRule(ctx context.Context, r *SqlInsightRuleRow) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_sql_insight_rules SET
  name=?, description=?, metric_key=?, rule_kind=?, direction=?,
  warn_threshold=?, crit_threshold=?, baseline_window=?, zscore_k=?,
  enabled=?, updated_by=?, updated_at=NOW()
WHERE id=? AND system=FALSE`),
		r.Name, r.Description, r.MetricKey, r.RuleKind, r.Direction,
		r.WarnThreshold, r.CritThreshold, r.BaselineWindow, r.ZScoreK,
		r.Enabled, r.UpdatedBy, r.ID)
	return err
}

func (d *DB) DeleteSqlInsightRule(ctx context.Context, id string) error {
	res, err := d.db.ExecContext(ctx, d.rewrite(`DELETE FROM tbl_sql_insight_rules WHERE id=? AND system=FALSE`), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("规则不存在或为系统预置不可删")
	}
	return nil
}

func (d *DB) GetSqlInsightRuleByID(ctx context.Context, id string) (*SqlInsightRuleRow, error) {
	row := d.db.QueryRowContext(ctx, d.rewrite("SELECT "+siRuleCols+" FROM tbl_sql_insight_rules WHERE id=?"), id)
	r, err := scanSqlInsightRule(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("规则不存在")
	}
	return r, err
}

func (d *DB) ListSqlInsightRules(ctx context.Context, metricKey string, enabledOnly bool, offset, limit int) ([]*SqlInsightRuleRow, int, error) {
	var conds []string
	var args []any
	if strings.TrimSpace(metricKey) != "" {
		conds = append(conds, "metric_key=?")
		args = append(args, metricKey)
	}
	if enabledOnly {
		conds = append(conds, "enabled=TRUE")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := d.db.QueryRowContext(ctx, d.rewrite("SELECT COUNT(*) FROM tbl_sql_insight_rules "+where), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	q := fmt.Sprintf(`SELECT %s FROM tbl_sql_insight_rules %s ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		siRuleCols, where)
	args = append(args, limit, offset)
	rows, err := d.db.QueryContext(ctx, d.rewrite(q), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*SqlInsightRuleRow
	for rows.Next() {
		r, err := scanSqlInsightRule(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (d *DB) ListSqlInsightEnabledRules(ctx context.Context) ([]*SqlInsightRuleRow, error) {
	list, _, err := d.ListSqlInsightRules(ctx, "", true, 0, 1000)
	return list, err
}

// ---------- Run ----------

type SqlInsightRunRow struct {
	ID               string
	AnalysisDate     time.Time
	Host             string
	Port             int
	Status           string
	Severity         string
	FindingCount     int
	CriticalCount    int
	WarningCount     int
	InfoCount        int
	ErrorMessage     string
	UserScenario     string
	ScenarioTags     json.RawMessage
	NarrativeStatus  string
	NarrativeJSON    json.RawMessage
	NarrativeText    string
	NarrativeSource  string
	TriggerType      string
	CreatedBy        string
	StartedAt        time.Time
	FinishedAt       *time.Time
	CreatedAt        time.Time
}

func (d *DB) InsertSqlInsightRun(ctx context.Context, r *SqlInsightRunRow) error {
	nj := r.NarrativeJSON
	if len(nj) == 0 {
		nj = json.RawMessage(`{}`)
	}
	tags := r.ScenarioTags
	if len(tags) == 0 {
		tags = json.RawMessage(`[]`)
	}
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_sql_insight_analysis_runs (
  id, analysis_date, host, port, status, severity, finding_count, critical_count, warning_count, info_count,
  error_message, user_scenario, scenario_tags, narrative_status, narrative_json, narrative_text, narrative_source,
  trigger_type, created_by, started_at, created_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NOW(),NOW())`),
		r.ID, r.AnalysisDate, r.Host, r.Port, r.Status, r.Severity, r.FindingCount, r.CriticalCount, r.WarningCount, r.InfoCount,
		r.ErrorMessage, r.UserScenario, tags, r.NarrativeStatus, nj, r.NarrativeText, r.NarrativeSource,
		r.TriggerType, r.CreatedBy)
	return err
}

func (d *DB) FinishSqlInsightRun(ctx context.Context, r *SqlInsightRunRow) error {
	nj := r.NarrativeJSON
	if len(nj) == 0 {
		nj = json.RawMessage(`{}`)
	}
	_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_sql_insight_analysis_runs SET
  status=?, severity=?, finding_count=?, critical_count=?, warning_count=?, info_count=?,
  error_message=?, narrative_status=?, narrative_json=?, narrative_text=?, narrative_source=?,
  finished_at=NOW()
WHERE id=?`),
		r.Status, r.Severity, r.FindingCount, r.CriticalCount, r.WarningCount, r.InfoCount,
		r.ErrorMessage, r.NarrativeStatus, nj, r.NarrativeText, r.NarrativeSource, r.ID)
	return err
}

func (d *DB) GetSqlInsightRun(ctx context.Context, id string) (*SqlInsightRunRow, error) {
	r := &SqlInsightRunRow{}
	var finished sql.NullTime
	var nj, tags []byte
	err := d.db.QueryRowContext(ctx, d.rewrite(`
SELECT id, analysis_date, host, port, status, severity, finding_count, critical_count, warning_count, info_count,
  error_message, COALESCE(user_scenario,''), COALESCE(scenario_tags,'[]'::jsonb),
  narrative_status, narrative_json, narrative_text, narrative_source, trigger_type, created_by,
  started_at, finished_at, created_at
FROM tbl_sql_insight_analysis_runs WHERE id=?`), id).Scan(
		&r.ID, &r.AnalysisDate, &r.Host, &r.Port, &r.Status, &r.Severity, &r.FindingCount, &r.CriticalCount, &r.WarningCount, &r.InfoCount,
		&r.ErrorMessage, &r.UserScenario, &tags, &r.NarrativeStatus, &nj, &r.NarrativeText, &r.NarrativeSource, &r.TriggerType, &r.CreatedBy,
		&r.StartedAt, &finished, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("分析运行不存在")
	}
	if err != nil {
		return nil, err
	}
	r.NarrativeJSON = nj
	r.ScenarioTags = tags
	if finished.Valid {
		t := finished.Time
		r.FinishedAt = &t
	}
	return r, nil
}

func (d *DB) ListSqlInsightRuns(ctx context.Context, host string, port int, offset, limit int) ([]*SqlInsightRunRow, int, error) {
	var conds []string
	var args []any
	if strings.TrimSpace(host) != "" {
		conds = append(conds, "host=?")
		args = append(args, host)
	}
	if port > 0 {
		conds = append(conds, "port=?")
		args = append(args, port)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := d.db.QueryRowContext(ctx, d.rewrite("SELECT COUNT(*) FROM tbl_sql_insight_analysis_runs "+where), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 20
	}
	q := fmt.Sprintf(`SELECT id, analysis_date, host, port, status, severity, finding_count, critical_count, warning_count, info_count,
  error_message, COALESCE(user_scenario,''), COALESCE(scenario_tags,'[]'::jsonb),
  narrative_status, narrative_json, narrative_text, narrative_source, trigger_type, created_by,
  started_at, finished_at, created_at
FROM tbl_sql_insight_analysis_runs %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, where)
	args = append(args, limit, offset)
	rows, err := d.db.QueryContext(ctx, d.rewrite(q), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*SqlInsightRunRow
	for rows.Next() {
		r := &SqlInsightRunRow{}
		var finished sql.NullTime
		var nj, tags []byte
		if err := rows.Scan(
			&r.ID, &r.AnalysisDate, &r.Host, &r.Port, &r.Status, &r.Severity, &r.FindingCount, &r.CriticalCount, &r.WarningCount, &r.InfoCount,
			&r.ErrorMessage, &r.UserScenario, &tags, &r.NarrativeStatus, &nj, &r.NarrativeText, &r.NarrativeSource, &r.TriggerType, &r.CreatedBy,
			&r.StartedAt, &finished, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		r.NarrativeJSON = nj
		r.ScenarioTags = tags
		if finished.Valid {
			t := finished.Time
			r.FinishedAt = &t
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// ---------- Finding ----------

type SqlInsightFindingRow struct {
	ID            string
	RunID         string
	AnalysisDate  time.Time
	Host          string
	Port          int
	InstanceID    string
	InstanceName  string
	MetricKey     string
	RuleID        string
	RuleKind      string
	Severity      string
	Title         string
	Message       string
	Value         *float64
	Threshold     *float64
	BaselineValue *float64
	Unit          string
	Evidence      json.RawMessage
	Suggestion    string
	Fingerprint   string
	Feedback      string
	FeedbackNote  string
	FeedbackBy    string
	FeedbackAt    *time.Time
	CreatedAt     time.Time
}

func (d *DB) InsertSqlInsightFindings(ctx context.Context, list []*SqlInsightFindingRow) error {
	if len(list) == 0 {
		return nil
	}
	for _, r := range list {
		ev := r.Evidence
		if len(ev) == 0 {
			ev = json.RawMessage(`{}`)
		}
		_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_sql_insight_findings (
  id, run_id, analysis_date, host, port, instance_id, instance_name, metric_key, rule_id, rule_kind,
  severity, title, message, value, threshold, baseline_value, unit, evidence, suggestion, fingerprint, created_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())`),
			r.ID, r.RunID, r.AnalysisDate, r.Host, r.Port, r.InstanceID, r.InstanceName, r.MetricKey, r.RuleID, r.RuleKind,
			r.Severity, r.Title, r.Message, r.Value, r.Threshold, r.BaselineValue, r.Unit, ev, r.Suggestion, r.Fingerprint)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ListSqlInsightFindings(ctx context.Context, runID, host, severity string, port, offset, limit int) ([]*SqlInsightFindingRow, int, error) {
	var conds []string
	var args []any
	if strings.TrimSpace(runID) != "" {
		conds = append(conds, "run_id=?")
		args = append(args, runID)
	}
	if strings.TrimSpace(host) != "" {
		conds = append(conds, "host=?")
		args = append(args, host)
	}
	if port > 0 {
		conds = append(conds, "port=?")
		args = append(args, port)
	}
	if strings.TrimSpace(severity) != "" {
		conds = append(conds, "severity=?")
		args = append(args, severity)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var total int
	if err := d.db.QueryRowContext(ctx, d.rewrite("SELECT COUNT(*) FROM tbl_sql_insight_findings "+where), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	q := fmt.Sprintf(`SELECT id, run_id, analysis_date, host, port, instance_id, instance_name, metric_key, rule_id, rule_kind,
  severity, title, message, value, threshold, baseline_value, unit, evidence, suggestion, fingerprint,
  feedback, feedback_note, feedback_by, feedback_at, created_at
FROM tbl_sql_insight_findings %s
ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END, created_at DESC
LIMIT ? OFFSET ?`, where)
	args = append(args, limit, offset)
	rows, err := d.db.QueryContext(ctx, d.rewrite(q), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*SqlInsightFindingRow
	for rows.Next() {
		r := &SqlInsightFindingRow{}
		var val, th, bl sql.NullFloat64
		var fbAt sql.NullTime
		var ev []byte
		if err := rows.Scan(
			&r.ID, &r.RunID, &r.AnalysisDate, &r.Host, &r.Port, &r.InstanceID, &r.InstanceName, &r.MetricKey, &r.RuleID, &r.RuleKind,
			&r.Severity, &r.Title, &r.Message, &val, &th, &bl, &r.Unit, &ev, &r.Suggestion, &r.Fingerprint,
			&r.Feedback, &r.FeedbackNote, &r.FeedbackBy, &fbAt, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		if val.Valid {
			v := val.Float64
			r.Value = &v
		}
		if th.Valid {
			v := th.Float64
			r.Threshold = &v
		}
		if bl.Valid {
			v := bl.Float64
			r.BaselineValue = &v
		}
		r.Evidence = ev
		if fbAt.Valid {
			t := fbAt.Time
			r.FeedbackAt = &t
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (d *DB) GetSqlInsightFinding(ctx context.Context, id string) (*SqlInsightFindingRow, error) {
	r := &SqlInsightFindingRow{}
	var val, th, bl sql.NullFloat64
	var fbAt sql.NullTime
	var ev []byte
	err := d.db.QueryRowContext(ctx, d.rewrite(`SELECT id, run_id, analysis_date, host, port, instance_id, instance_name, metric_key, rule_id, rule_kind,
  severity, title, message, value, threshold, baseline_value, unit, evidence, suggestion, fingerprint,
  feedback, feedback_note, feedback_by, feedback_at, created_at
FROM tbl_sql_insight_findings WHERE id=?`), id).Scan(
		&r.ID, &r.RunID, &r.AnalysisDate, &r.Host, &r.Port, &r.InstanceID, &r.InstanceName, &r.MetricKey, &r.RuleID, &r.RuleKind,
		&r.Severity, &r.Title, &r.Message, &val, &th, &bl, &r.Unit, &ev, &r.Suggestion, &r.Fingerprint,
		&r.Feedback, &r.FeedbackNote, &r.FeedbackBy, &fbAt, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("发现不存在")
	}
	if err != nil {
		return nil, err
	}
	if val.Valid {
		v := val.Float64
		r.Value = &v
	}
	if th.Valid {
		v := th.Float64
		r.Threshold = &v
	}
	if bl.Valid {
		v := bl.Float64
		r.BaselineValue = &v
	}
	r.Evidence = ev
	if fbAt.Valid {
		t := fbAt.Time
		r.FeedbackAt = &t
	}
	return r, nil
}

func (d *DB) UpdateSqlInsightFindingFeedback(ctx context.Context, id, feedback, note, by string) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
UPDATE tbl_sql_insight_findings SET feedback=?, feedback_note=?, feedback_by=?, feedback_at=NOW()
WHERE id=?`), feedback, note, by, id)
	return err
}

// ---------- Observation / Baseline ----------

func (d *DB) UpsertSqlInsightObservation(ctx context.Context, runID string, day time.Time, host string, port int, metricKey string, value float64, unit string) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_sql_insight_observations (run_id, obs_date, host, port, metric_key, value, unit, created_at)
VALUES (?,?,?,?,?,?,?,NOW())
ON CONFLICT (obs_date, host, port, metric_key) DO UPDATE SET
  run_id=EXCLUDED.run_id, value=EXCLUDED.value, unit=EXCLUDED.unit`),
		runID, day, host, port, metricKey, value, unit)
	return err
}

type SqlInsightBaselineRow struct {
	Host        string
	Port        int
	MetricKey   string
	WindowKey   string
	SampleCount int
	Mean        *float64
	Stddev      *float64
	P50         *float64
	P95         *float64
	P99         *float64
	Min         *float64
	Max         *float64
	LastValue   *float64
	UpdatedAt   time.Time
}

func (d *DB) UpsertSqlInsightBaseline(ctx context.Context, b *SqlInsightBaselineRow) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_sql_insight_metric_baselines (
  host, port, metric_key, window_key, sample_count, mean_val, stddev_val, p50, p95, p99, min_val, max_val, last_value, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())
ON CONFLICT (host, port, metric_key, window_key) DO UPDATE SET
  sample_count=EXCLUDED.sample_count, mean_val=EXCLUDED.mean_val, stddev_val=EXCLUDED.stddev_val,
  p50=EXCLUDED.p50, p95=EXCLUDED.p95, p99=EXCLUDED.p99, min_val=EXCLUDED.min_val, max_val=EXCLUDED.max_val,
  last_value=EXCLUDED.last_value, updated_at=NOW()`),
		b.Host, b.Port, b.MetricKey, b.WindowKey, b.SampleCount, b.Mean, b.Stddev, b.P50, b.P95, b.P99, b.Min, b.Max, b.LastValue)
	return err
}

func (d *DB) GetSqlInsightBaseline(ctx context.Context, host string, port int, metricKey, windowKey string) (*SqlInsightBaselineRow, error) {
	b := &SqlInsightBaselineRow{}
	var mean, std, p50, p95, p99, mn, mx, last sql.NullFloat64
	err := d.db.QueryRowContext(ctx, d.rewrite(`
SELECT host, port, metric_key, window_key, sample_count, mean_val, stddev_val, p50, p95, p99, min_val, max_val, last_value, updated_at
FROM tbl_sql_insight_metric_baselines WHERE host=? AND port=? AND metric_key=? AND window_key=?`),
		host, port, metricKey, windowKey).Scan(
		&b.Host, &b.Port, &b.MetricKey, &b.WindowKey, &b.SampleCount, &mean, &std, &p50, &p95, &p99, &mn, &mx, &last, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Mean = siNullF(mean)
	b.Stddev = siNullF(std)
	b.P50 = siNullF(p50)
	b.P95 = siNullF(p95)
	b.P99 = siNullF(p99)
	b.Min = siNullF(mn)
	b.Max = siNullF(mx)
	b.LastValue = siNullF(last)
	return b, nil
}

func siNullF(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func (d *DB) ListSqlInsightObservationValues(ctx context.Context, host string, port int, metricKey string, since time.Time) ([]float64, error) {
	rows, err := d.db.QueryContext(ctx, d.rewrite(`
SELECT value FROM tbl_sql_insight_observations
WHERE host=? AND port=? AND metric_key=? AND obs_date >= ?
ORDER BY obs_date`), host, port, metricKey, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------- Case ----------

type SqlInsightCaseRow struct {
	ID                 string
	Fingerprint        string
	MetricKey          string
	Severity           string
	Direction          string
	Title              string
	RootCauseTemplate  string
	ActionTemplate     string
	HitCount           int
	ConfirmCount       int
	DismissCount       int
	System             bool
	LastSeenAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          *time.Time
}

func (d *DB) UpsertSqlInsightCase(ctx context.Context, c *SqlInsightCaseRow) error {
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_sql_insight_cases (
  id, fingerprint, metric_key, severity, direction, title, root_cause_template, action_template,
  hit_count, confirm_count, dismiss_count, system, last_seen_at, created_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())
ON CONFLICT (fingerprint) DO UPDATE SET
  title=EXCLUDED.title, root_cause_template=EXCLUDED.root_cause_template, action_template=EXCLUDED.action_template,
  hit_count=tbl_sql_insight_cases.hit_count + 1, last_seen_at=EXCLUDED.last_seen_at, updated_at=NOW()`),
		c.ID, c.Fingerprint, c.MetricKey, c.Severity, c.Direction, c.Title, c.RootCauseTemplate, c.ActionTemplate,
		c.HitCount, c.ConfirmCount, c.DismissCount, c.System, c.LastSeenAt)
	return err
}

func (d *DB) GetSqlInsightCaseByFingerprint(ctx context.Context, fp string) (*SqlInsightCaseRow, error) {
	c := &SqlInsightCaseRow{}
	var last, updated sql.NullTime
	err := d.db.QueryRowContext(ctx, d.rewrite(`
SELECT id, fingerprint, metric_key, severity, direction, title, root_cause_template, action_template,
  hit_count, confirm_count, dismiss_count, system, last_seen_at, created_at, updated_at
FROM tbl_sql_insight_cases WHERE fingerprint=?`), fp).Scan(
		&c.ID, &c.Fingerprint, &c.MetricKey, &c.Severity, &c.Direction, &c.Title, &c.RootCauseTemplate, &c.ActionTemplate,
		&c.HitCount, &c.ConfirmCount, &c.DismissCount, &c.System, &last, &c.CreatedAt, &updated)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if last.Valid {
		t := last.Time
		c.LastSeenAt = &t
	}
	if updated.Valid {
		t := updated.Time
		c.UpdatedAt = &t
	}
	return c, nil
}

func (d *DB) IncrSqlInsightCaseFeedback(ctx context.Context, fp, kind string) error {
	col := "confirm_count"
	if kind == "dismiss" {
		col = "dismiss_count"
	}
	_, err := d.db.ExecContext(ctx, d.rewrite(fmt.Sprintf(`
UPDATE tbl_sql_insight_cases SET %s = %s + 1, updated_at=NOW() WHERE fingerprint=?`, col, col)), fp)
	return err
}

func (d *DB) ListSqlInsightCases(ctx context.Context, offset, limit int) ([]*SqlInsightCaseRow, int, error) {
	var total int
	if err := d.db.QueryRowContext(ctx, d.rewrite(`SELECT COUNT(*) FROM tbl_sql_insight_cases`)).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.QueryContext(ctx, d.rewrite(`
SELECT id, fingerprint, metric_key, severity, direction, title, root_cause_template, action_template,
  hit_count, confirm_count, dismiss_count, system, last_seen_at, created_at, updated_at
FROM tbl_sql_insight_cases ORDER BY confirm_count DESC, hit_count DESC LIMIT ? OFFSET ?`), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*SqlInsightCaseRow
	for rows.Next() {
		c := &SqlInsightCaseRow{}
		var last, updated sql.NullTime
		if err := rows.Scan(
			&c.ID, &c.Fingerprint, &c.MetricKey, &c.Severity, &c.Direction, &c.Title, &c.RootCauseTemplate, &c.ActionTemplate,
			&c.HitCount, &c.ConfirmCount, &c.DismissCount, &c.System, &last, &c.CreatedAt, &updated); err != nil {
			return nil, 0, err
		}
		if last.Valid {
			t := last.Time
			c.LastSeenAt = &t
		}
		if updated.Valid {
			t := updated.Time
			c.UpdatedAt = &t
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// PGActivityDayMaxXact 某日某实例最长事务时长（秒）。
func (d *DB) PGActivityDayMaxXact(instanceID string, dayStart, dayEnd time.Time) (float64, bool, error) {
	var v sql.NullFloat64
	err := d.QueryRow(`
SELECT MAX(xact_duration) FROM tbl_pg_activity_snapshots
WHERE instance_id=? AND collected_at >= ? AND collected_at < ?`,
		instanceID, dayStart.UTC().Format(time.RFC3339), dayEnd.UTC().Format(time.RFC3339)).Scan(&v)
	if err != nil {
		return 0, false, err
	}
	if !v.Valid {
		return 0, false, nil
	}
	return v.Float64, true, nil
}

// PGActivityDayActiveCount 某日某实例活跃会话快照条数。
func (d *DB) PGActivityDayActiveCount(instanceID string, dayStart, dayEnd time.Time) (int, error) {
	var n int
	err := d.QueryRow(`
SELECT COUNT(1) FROM tbl_pg_activity_snapshots
WHERE instance_id=? AND collected_at >= ? AND collected_at < ?`,
		instanceID, dayStart.UTC().Format(time.RFC3339), dayEnd.UTC().Format(time.RFC3339)).Scan(&n)
	return n, err
}
