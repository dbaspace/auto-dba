package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AWRReportRow 一条每日 AWR 报告记录。
type AWRReportRow struct {
	ID           int64           `json:"id"`
	ReportDate   time.Time       `json:"report_date"`
	InstanceID   string          `json:"instance_id"`
	InstanceName string          `json:"instance_name"`
	Host         string          `json:"host"`
	Port         int64           `json:"port"`
	Source       string          `json:"source"`
	PeriodStart  time.Time       `json:"period_start"`
	PeriodEnd    time.Time       `json:"period_end"`
	Status       string          `json:"status"`
	Severity     string          `json:"severity"`
	AnomalyCount int             `json:"anomaly_count"`
	Anomalies    json.RawMessage `json:"anomalies"`
	ErrorMessage string          `json:"error_message"`
	Sections     json.RawMessage `json:"sections"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    *time.Time      `json:"updated_at"`
}

// AWRListFilter 报告列表过滤。
type AWRListFilter struct {
	ReportDate *time.Time
	StartDate  *time.Time
	EndDate    *time.Time
	InstanceID string
	Host       string
	Port       int64
	Status     string
	Offset     int
	Limit      int
}

// AWRGetFilter 报告详情定位。
type AWRGetFilter struct {
	ID         int64
	ReportDate *time.Time
	Host       string
	Port       int64
}

// UpsertAWRReport 按 (report_date, host, port) upsert。
func (d *DB) UpsertAWRReport(ctx context.Context, r *AWRReportRow) error {
	sections := r.Sections
	if len(sections) == 0 {
		sections = json.RawMessage(`{}`)
	}
	anomalies := r.Anomalies
	if len(anomalies) == 0 {
		anomalies = json.RawMessage(`[]`)
	}
	source := r.Source
	if strings.TrimSpace(source) == "" {
		source = "unknown"
	}
	severity := r.Severity
	if strings.TrimSpace(severity) == "" {
		severity = "none"
	}
	_, err := d.db.ExecContext(ctx, d.rewrite(`
INSERT INTO tbl_db_awr_reports (
  report_date, instance_id, instance_name, host, port, source,
  period_start, period_end, status, severity, anomaly_count, anomalies,
  error_message, sections, created_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,NOW())
ON CONFLICT (report_date, host, port) DO UPDATE SET
  instance_id   = EXCLUDED.instance_id,
  instance_name = EXCLUDED.instance_name,
  source        = EXCLUDED.source,
  period_start  = EXCLUDED.period_start,
  period_end    = EXCLUDED.period_end,
  status        = EXCLUDED.status,
  severity      = EXCLUDED.severity,
  anomaly_count = EXCLUDED.anomaly_count,
  anomalies     = EXCLUDED.anomalies,
  error_message = EXCLUDED.error_message,
  sections      = EXCLUDED.sections,
  created_at    = NOW(),
  updated_at    = NOW()`),
		r.ReportDate, r.InstanceID, r.InstanceName, r.Host, r.Port, source,
		r.PeriodStart, r.PeriodEnd, r.Status, severity, r.AnomalyCount, []byte(anomalies),
		r.ErrorMessage, []byte(sections))
	return err
}

// ListAWRReports 分页查询（不含 sections）。
func (d *DB) ListAWRReports(ctx context.Context, f AWRListFilter) ([]*AWRReportRow, int, error) {
	var conds []string
	var args []any
	if f.ReportDate != nil {
		conds = append(conds, "report_date = ?")
		args = append(args, *f.ReportDate)
	}
	if f.StartDate != nil {
		conds = append(conds, "report_date >= ?")
		args = append(args, *f.StartDate)
	}
	if f.EndDate != nil {
		conds = append(conds, "report_date <= ?")
		args = append(args, *f.EndDate)
	}
	if strings.TrimSpace(f.InstanceID) != "" {
		conds = append(conds, "instance_id = ?")
		args = append(args, f.InstanceID)
	}
	if strings.TrimSpace(f.Host) != "" {
		conds = append(conds, "host = ?")
		args = append(args, f.Host)
	}
	if f.Port > 0 {
		conds = append(conds, "port = ?")
		args = append(args, f.Port)
	}
	if strings.TrimSpace(f.Status) != "" {
		conds = append(conds, "status = ?")
		args = append(args, f.Status)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	if err := d.db.QueryRowContext(ctx, d.rewrite("SELECT COUNT(*) FROM tbl_db_awr_reports "+where), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*AWRReportRow{}, 0, nil
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	querySQL := `
SELECT id, report_date, COALESCE(instance_id, ''), COALESCE(instance_name, ''), host, port,
  COALESCE(source, 'unknown'), period_start, period_end, status,
  COALESCE(severity, 'none'), COALESCE(anomaly_count, 0), COALESCE(anomalies, '[]'::jsonb),
  COALESCE(error_message, ''), created_at, updated_at
FROM tbl_db_awr_reports ` + where + `
ORDER BY report_date DESC, host ASC, port ASC
LIMIT ? OFFSET ?`
	args = append(args, limit, f.Offset)

	rows, err := d.db.QueryContext(ctx, d.rewrite(querySQL), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []*AWRReportRow
	for rows.Next() {
		var r AWRReportRow
		var updatedAt sql.NullTime
		var anomalies []byte
		if err := rows.Scan(
			&r.ID, &r.ReportDate, &r.InstanceID, &r.InstanceName, &r.Host, &r.Port,
			&r.Source, &r.PeriodStart, &r.PeriodEnd, &r.Status,
			&r.Severity, &r.AnomalyCount, &anomalies,
			&r.ErrorMessage, &r.CreatedAt, &updatedAt,
		); err != nil {
			return nil, 0, err
		}
		r.Anomalies = json.RawMessage(anomalies)
		if updatedAt.Valid {
			t := updatedAt.Time
			r.UpdatedAt = &t
		}
		list = append(list, &r)
	}
	return list, total, rows.Err()
}

// GetAWRReport 按 ID 或 report_date+host+port 查询（含 sections）。
func (d *DB) GetAWRReport(ctx context.Context, f AWRGetFilter) (*AWRReportRow, error) {
	var conds []string
	var args []any
	if f.ID > 0 {
		conds = append(conds, "id = ?")
		args = append(args, f.ID)
	} else {
		if f.ReportDate == nil || strings.TrimSpace(f.Host) == "" || f.Port <= 0 {
			return nil, fmt.Errorf("get awr report: need id or (report_date, host, port)")
		}
		conds = append(conds, "report_date = ?")
		args = append(args, *f.ReportDate)
		conds = append(conds, "host = ?")
		args = append(args, f.Host)
		conds = append(conds, "port = ?")
		args = append(args, f.Port)
	}
	where := "WHERE " + strings.Join(conds, " AND ")

	var r AWRReportRow
	var sections []byte
	var anomalies []byte
	var updatedAt sql.NullTime
	err := d.db.QueryRowContext(ctx, d.rewrite(`
SELECT id, report_date, COALESCE(instance_id, ''), COALESCE(instance_name, ''), host, port,
  COALESCE(source, 'unknown'), period_start, period_end, status,
  COALESCE(severity, 'none'), COALESCE(anomaly_count, 0), COALESCE(anomalies, '[]'::jsonb),
  COALESCE(error_message, ''), sections, created_at, updated_at
FROM tbl_db_awr_reports `+where+` LIMIT 1`), args...).Scan(
		&r.ID, &r.ReportDate, &r.InstanceID, &r.InstanceName, &r.Host, &r.Port,
		&r.Source, &r.PeriodStart, &r.PeriodEnd, &r.Status,
		&r.Severity, &r.AnomalyCount, &anomalies,
		&r.ErrorMessage, &sections, &r.CreatedAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.Sections = json.RawMessage(sections)
	r.Anomalies = json.RawMessage(anomalies)
	if updatedAt.Valid {
		t := updatedAt.Time
		r.UpdatedAt = &t
	}
	return &r, nil
}

// AWRAnomalySummaryFilter 异常汇总过滤。
type AWRAnomalySummaryFilter struct {
	ReportDate    *time.Time
	StartDate     *time.Time
	EndDate       *time.Time
	Severity      string
	Limit         int
	IncludeFailed bool
}

// AWRAnomalySummaryResult 异常汇总结果。
type AWRAnomalySummaryResult struct {
	Total         int
	CriticalCount int
	WarningCount  int
	NoneCount     int
	FailedCount   int
	AnomalyTotal  int
	TopInstances  []*AWRReportRow
}

// AnomalySummaryAWR 按日期聚合异常概览。
func (d *DB) AnomalySummaryAWR(ctx context.Context, f AWRAnomalySummaryFilter) (*AWRAnomalySummaryResult, error) {
	var conds []string
	var args []any
	if f.ReportDate != nil {
		conds = append(conds, "report_date = ?")
		args = append(args, *f.ReportDate)
	}
	if f.StartDate != nil {
		conds = append(conds, "report_date >= ?")
		args = append(args, *f.StartDate)
	}
	if f.EndDate != nil {
		conds = append(conds, "report_date <= ?")
		args = append(args, *f.EndDate)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	res := &AWRAnomalySummaryResult{}
	aggSQL := `
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN severity = 'critical' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN severity = 'warning'  THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN severity = 'none' OR severity IS NULL THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(COALESCE(anomaly_count, 0)), 0)
FROM tbl_db_awr_reports ` + where
	if err := d.db.QueryRowContext(ctx, d.rewrite(aggSQL), args...).Scan(
		&res.Total, &res.CriticalCount, &res.WarningCount, &res.NoneCount, &res.FailedCount, &res.AnomalyTotal,
	); err != nil {
		return nil, err
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	topConds := append([]string{}, conds...)
	topArgs := append([]any{}, args...)
	if strings.TrimSpace(f.Severity) != "" {
		topConds = append(topConds, "severity = ?")
		topArgs = append(topArgs, f.Severity)
	} else if f.IncludeFailed {
		topConds = append(topConds, "(severity IN ('warning','critical') OR status = 'failed')")
	} else {
		topConds = append(topConds, "severity <> 'none'")
	}
	topWhere := "WHERE " + strings.Join(topConds, " AND ")
	topSQL := `
SELECT id, report_date, COALESCE(instance_id, ''), COALESCE(instance_name, ''), host, port,
  COALESCE(source, 'unknown'), period_start, period_end, status,
  COALESCE(severity, 'none'), COALESCE(anomaly_count, 0), COALESCE(anomalies, '[]'::jsonb),
  COALESCE(error_message, ''), created_at, updated_at
FROM tbl_db_awr_reports ` + topWhere + `
ORDER BY (CASE
    WHEN severity = 'critical' THEN 3
    WHEN severity = 'warning'  THEN 2
    WHEN status = 'failed'     THEN 1
    ELSE 0 END) DESC,
  anomaly_count DESC, report_date DESC, host ASC, port ASC
LIMIT ?`
	topArgs = append(topArgs, limit)

	rows, err := d.db.QueryContext(ctx, d.rewrite(topSQL), topArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r AWRReportRow
		var updatedAt sql.NullTime
		var anomalies []byte
		if err := rows.Scan(
			&r.ID, &r.ReportDate, &r.InstanceID, &r.InstanceName, &r.Host, &r.Port,
			&r.Source, &r.PeriodStart, &r.PeriodEnd, &r.Status,
			&r.Severity, &r.AnomalyCount, &anomalies,
			&r.ErrorMessage, &r.CreatedAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		r.Anomalies = json.RawMessage(anomalies)
		if updatedAt.Valid {
			t := updatedAt.Time
			r.UpdatedAt = &t
		}
		res.TopInstances = append(res.TopInstances, &r)
	}
	return res, rows.Err()
}

// DeleteAWRReportsBefore 删除 report_date 早于 before 的报告。
func (d *DB) DeleteAWRReportsBefore(ctx context.Context, before time.Time) (int64, error) {
	res, err := d.db.ExecContext(ctx, d.rewrite("DELETE FROM tbl_db_awr_reports WHERE report_date < ?"), before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
