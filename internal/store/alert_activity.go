package store

import "time"

// ActivityWindow 实时动态的查询窗口。Since/Until 是 RFC3339 文本，用于告警、工单、HA、CMDB。
// SinceAt/UntilAt 用于 AWR 的 timestamptz。
type ActivityWindow struct {
	Since      string
	Until      string
	SinceAt    time.Time
	UntilAt    time.Time
	TenantID   string
	InstanceID string
	Severity   string
	Limit      int
	Actions    []string // 告警事件 action，空则 trigger 与 resolve
}

func (w ActivityWindow) limit() int {
	if w.Limit <= 0 {
		return 50
	}
	if w.Limit > 200 {
		return 200
	}
	return w.Limit
}

// AlertActivityRow 一条触发或恢复事件，带上父告警的当前处理状态。
type AlertActivityRow struct {
	EventID   string
	Action    string
	Detail    string
	Operator  string
	EventAt   string
	AlertID   string
	Severity  string
	Title     string
	Handle    string
	Owner     string
	CurValue  string
	Threshold string
	Labels    string
}

func (d *DB) ListAlertActivity(w ActivityWindow) ([]AlertActivityRow, error) {
	q := `SELECT e.id, e.action, e.detail, e.operator, e.created_at,
		a.id, a.severity, a.title, a.handle, a.owner, a.cur_value, a.threshold, a.labels
		FROM tbl_alert_events e JOIN tbl_alerts a ON a.id = e.alert_id
		WHERE e.created_at >= ? AND e.created_at < ?`
	args := []any{w.Since, w.Until}
	q, args = activityActions(q, args, w.Actions)
	q, args = activityTenant(q, args, "a.tenant_id", w.TenantID)
	if w.Severity != "" {
		q += ` AND a.severity=?`
		args = append(args, w.Severity)
	}
	q, args = activityLabelInstance(q, args, w.InstanceID)
	q += ` ORDER BY e.created_at DESC LIMIT ?`
	args = append(args, w.limit())
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertActivityRow
	for rows.Next() {
		var r AlertActivityRow
		if err := rows.Scan(&r.EventID, &r.Action, &r.Detail, &r.Operator, &r.EventAt,
			&r.AlertID, &r.Severity, &r.Title, &r.Handle, &r.Owner, &r.CurValue, &r.Threshold, &r.Labels); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) CountAlertActivity(w ActivityWindow) (int, error) {
	q := `SELECT COUNT(1) FROM tbl_alert_events e JOIN tbl_alerts a ON a.id = e.alert_id
		WHERE e.created_at >= ? AND e.created_at < ?`
	args := []any{w.Since, w.Until}
	q, args = activityActions(q, args, w.Actions)
	q, args = activityTenant(q, args, "a.tenant_id", w.TenantID)
	if w.Severity != "" {
		q += ` AND a.severity=?`
		args = append(args, w.Severity)
	}
	q, args = activityLabelInstance(q, args, w.InstanceID)
	var n int
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

// TicketActivityRow 已执行或失败、且挂了实例的工单。
type TicketActivityRow struct {
	ID          string
	TicketNo    string
	IssueType   string
	InstanceID  string
	Description string
	Status      string
	Submitter   string
	UpdatedAt   string
}

func (d *DB) ListTicketActivity(w ActivityWindow) ([]TicketActivityRow, error) {
	q := `SELECT id, ticket_no, issue_type, COALESCE(instance_id,''), COALESCE(description,''), status, submitter, updated_at
		FROM tbl_issues
		WHERE status IN (?,?) AND updated_at >= ? AND updated_at < ? AND COALESCE(instance_id,'')<>''`
	args := []any{StatusExecuted, StatusFailed, w.Since, w.Until}
	q, args = activityTenant(q, args, "tenant_id", w.TenantID)
	if w.InstanceID != "" {
		q += ` AND instance_id=?`
		args = append(args, w.InstanceID)
	}
	q += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, w.limit())
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketActivityRow
	for rows.Next() {
		var r TicketActivityRow
		if err := rows.Scan(&r.ID, &r.TicketNo, &r.IssueType, &r.InstanceID, &r.Description, &r.Status, &r.Submitter, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) CountTicketActivity(w ActivityWindow) (int, error) {
	q := `SELECT COUNT(1) FROM tbl_issues WHERE status IN (?,?) AND updated_at >= ? AND updated_at < ? AND COALESCE(instance_id,'')<>''`
	args := []any{StatusExecuted, StatusFailed, w.Since, w.Until}
	q, args = activityTenant(q, args, "tenant_id", w.TenantID)
	if w.InstanceID != "" {
		q += ` AND instance_id=?`
		args = append(args, w.InstanceID)
	}
	var n int
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

// HAActivityRow 已完成或失败的高可用任务。
type HAActivityRow struct {
	ID               string
	Action           string
	Status           string
	TargetInstanceID string
	Operator         string
	UpdatedAt        string
	ClusterName      string
}

func (d *DB) ListHAActivity(w ActivityWindow) ([]HAActivityRow, error) {
	q := `SELECT t.id, t.action, t.status, COALESCE(t.target_instance_id,''), COALESCE(t.operator,''), t.updated_at, COALESCE(c.name,'')
		FROM tbl_ha_tasks t LEFT JOIN tbl_ha_clusters c ON c.id = t.cluster_id
		WHERE t.status IN (?,?) AND t.updated_at >= ? AND t.updated_at < ?`
	args := []any{StatusExecuted, StatusFailed, w.Since, w.Until}
	q, args = activityTenant(q, args, "t.tenant_id", w.TenantID)
	if w.InstanceID != "" {
		q += ` AND t.target_instance_id=?`
		args = append(args, w.InstanceID)
	}
	q += ` ORDER BY t.updated_at DESC LIMIT ?`
	args = append(args, w.limit())
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HAActivityRow
	for rows.Next() {
		var r HAActivityRow
		if err := rows.Scan(&r.ID, &r.Action, &r.Status, &r.TargetInstanceID, &r.Operator, &r.UpdatedAt, &r.ClusterName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) CountHAActivity(w ActivityWindow) (int, error) {
	q := `SELECT COUNT(1) FROM tbl_ha_tasks t WHERE t.status IN (?,?) AND t.updated_at >= ? AND t.updated_at < ?`
	args := []any{StatusExecuted, StatusFailed, w.Since, w.Until}
	q, args = activityTenant(q, args, "t.tenant_id", w.TenantID)
	if w.InstanceID != "" {
		q += ` AND t.target_instance_id=?`
		args = append(args, w.InstanceID)
	}
	var n int
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

// ConfigActivityRow 实例 CI 的配置审计。
type ConfigActivityRow struct {
	ID        string
	Action    string
	Actor     string
	CIID      string
	Source    string
	CreatedAt string
}

func (d *DB) ListConfigActivity(w ActivityWindow) ([]ConfigActivityRow, error) {
	q := `SELECT id, action, actor, ci_id, source, created_at FROM tbl_cmdb_audit_logs
		WHERE ci_type='instance' AND created_at >= ? AND created_at < ?`
	args := []any{w.Since, w.Until}
	q, args = activityTenant(q, args, "tenant_id", w.TenantID)
	if w.InstanceID != "" {
		q += ` AND ci_id=?`
		args = append(args, w.InstanceID)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, w.limit())
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConfigActivityRow
	for rows.Next() {
		var r ConfigActivityRow
		if err := rows.Scan(&r.ID, &r.Action, &r.Actor, &r.CIID, &r.Source, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) CountConfigActivity(w ActivityWindow) (int, error) {
	q := `SELECT COUNT(1) FROM tbl_cmdb_audit_logs WHERE ci_type='instance' AND created_at >= ? AND created_at < ?`
	args := []any{w.Since, w.Until}
	q, args = activityTenant(q, args, "tenant_id", w.TenantID)
	if w.InstanceID != "" {
		q += ` AND ci_id=?`
		args = append(args, w.InstanceID)
	}
	var n int
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

// AWRActivityRow 某一天的巡检汇总。
type AWRActivityRow struct {
	Date    string
	Total   int
	Anomaly int
	At      time.Time
}

func (d *DB) ListAWRActivity(w ActivityWindow) ([]AWRActivityRow, error) {
	q := `SELECT to_char(r.report_date, 'YYYY-MM-DD'), COUNT(1),
		COUNT(1) FILTER (WHERE r.anomaly_count > 0), MAX(r.created_at)
		FROM tbl_db_awr_reports r
		LEFT JOIN tbl_instances i ON i.id = r.instance_id
		WHERE r.created_at >= ? AND r.created_at < ?`
	args := []any{w.SinceAt, w.UntilAt}
	if w.TenantID != "" {
		q += ` AND COALESCE(i.tenant_id,'')=?`
		args = append(args, w.TenantID)
	}
	if w.InstanceID != "" {
		q += ` AND r.instance_id=?`
		args = append(args, w.InstanceID)
	}
	q += ` GROUP BY r.report_date ORDER BY r.report_date DESC LIMIT ?`
	args = append(args, w.limit())
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AWRActivityRow
	for rows.Next() {
		var r AWRActivityRow
		if err := rows.Scan(&r.Date, &r.Total, &r.Anomaly, &r.At); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) CountAWRActivity(w ActivityWindow) (int, error) {
	q := `SELECT COUNT(DISTINCT r.report_date) FROM tbl_db_awr_reports r
		LEFT JOIN tbl_instances i ON i.id = r.instance_id
		WHERE r.created_at >= ? AND r.created_at < ?`
	args := []any{w.SinceAt, w.UntilAt}
	if w.TenantID != "" {
		q += ` AND COALESCE(i.tenant_id,'')=?`
		args = append(args, w.TenantID)
	}
	if w.InstanceID != "" {
		q += ` AND r.instance_id=?`
		args = append(args, w.InstanceID)
	}
	var n int
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

func activityActions(q string, args []any, actions []string) (string, []any) {
	if len(actions) == 0 {
		actions = []string{"trigger", "resolve"}
	}
	if len(actions) == 1 {
		return q + ` AND e.action=?`, append(args, actions[0])
	}
	return q + ` AND e.action IN ('trigger','resolve')`, args
}

func activityTenant(q string, args []any, col, tenantID string) (string, []any) {
	if tenantID == "" {
		return q, args
	}
	return q + ` AND COALESCE(` + col + `,'')=?`, append(args, tenantID)
}

func activityLabelInstance(q string, args []any, instanceID string) (string, []any) {
	if instanceID == "" {
		return q, args
	}
	q += ` AND (a.labels LIKE ? OR a.labels LIKE ? OR a.labels LIKE ?)`
	args = append(args,
		`%"instance_id":"`+instanceID+`"`,
		`%"instanceId":"`+instanceID+`"`,
		`%"ci_id":"`+instanceID+`"`,
	)
	return q, args
}
