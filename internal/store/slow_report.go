package store

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SlowReport 一条慢 SQL 周报规则。
type SlowReport struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	EnvIDs      []string `json:"env_ids"`
	TenantID    string   `json:"tenant_id"`
	InstanceIDs []string `json:"instance_ids"`
	DBNames     []string `json:"db_names"`
	CronExpr    string   `json:"cron_expr"`
	Recipients  string   `json:"recipients"`
	TopN        int      `json:"top_n"`
	LastSentAt  string   `json:"last_sent_at"`
	LastFireKey string   `json:"last_fire_key"`
	LastError   string   `json:"last_error"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// SlowReportMail 一次已生成的周报 HTML。
type SlowReportMail struct {
	ID          string `json:"id"`
	ReportID    string `json:"report_id"`
	SentAt      string `json:"sent_at"`
	Trigger     string `json:"trigger"`
	EnvLabel    string `json:"env_label"`
	TenantLabel string `json:"tenant_label"`
	Recipients  string `json:"recipients"`
	Subject     string `json:"subject"`
	HTML        string `json:"html,omitempty"`
	OK          bool   `json:"ok"`
	Error       string `json:"error"`
}

func normalizeSlowReport(r *SlowReport) {
	if r.EnvIDs == nil {
		r.EnvIDs = []string{}
	}
	if r.InstanceIDs == nil {
		r.InstanceIDs = []string{}
	}
	if r.DBNames == nil {
		r.DBNames = []string{}
	}
	r.CronExpr = strings.TrimSpace(r.CronExpr)
	if r.CronExpr == "" {
		r.CronExpr = "0 9 * * 1"
	}
	if r.TopN <= 0 {
		r.TopN = 20
	}
	if r.TopN > 100 {
		r.TopN = 100
	}
	r.Name = strings.TrimSpace(r.Name)
	r.TenantID = strings.TrimSpace(r.TenantID)
	r.Recipients = strings.TrimSpace(r.Recipients)
}

func (d *DB) ListSlowReports() ([]SlowReport, error) {
	rows, err := d.Query(`SELECT id, name, enabled, env_ids, tenant_id, instance_ids, db_names, cron_expr, recipients, top_n,
		last_sent_at, last_fire_key, last_error, created_at, updated_at
		FROM tbl_cloud_slow_reports ORDER BY updated_at DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SlowReport{}
	for rows.Next() {
		r, err := scanSlowReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) GetSlowReport(id string) (*SlowReport, error) {
	row := d.QueryRow(`SELECT id, name, enabled, env_ids, tenant_id, instance_ids, db_names, cron_expr, recipients, top_n,
		last_sent_at, last_fire_key, last_error, created_at, updated_at
		FROM tbl_cloud_slow_reports WHERE id=?`, strings.TrimSpace(id))
	r, err := scanSlowReport(row)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

type slowReportScanner interface {
	Scan(dest ...any) error
}

func scanSlowReport(sc slowReportScanner) (SlowReport, error) {
	var r SlowReport
	var en int
	var envIDs, instIDs, dbNames string
	err := sc.Scan(&r.ID, &r.Name, &en, &envIDs, &r.TenantID, &instIDs, &dbNames, &r.CronExpr, &r.Recipients, &r.TopN,
		&r.LastSentAt, &r.LastFireKey, &r.LastError, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return r, err
	}
	r.Enabled = en != 0
	_ = json.Unmarshal([]byte(envIDs), &r.EnvIDs)
	_ = json.Unmarshal([]byte(instIDs), &r.InstanceIDs)
	_ = json.Unmarshal([]byte(dbNames), &r.DBNames)
	normalizeSlowReport(&r)
	return r, nil
}

func (d *DB) SaveSlowReport(r *SlowReport) error {
	normalizeSlowReport(r)
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(r.ID) == "" {
		r.ID = uuid.NewString()
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	envIDs, _ := json.Marshal(r.EnvIDs)
	instIDs, _ := json.Marshal(r.InstanceIDs)
	dbNames, _ := json.Marshal(r.DBNames)
	en := 0
	if r.Enabled {
		en = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_cloud_slow_reports(
		id, name, enabled, env_ids, tenant_id, instance_ids, db_names, cron_expr, recipients, top_n,
		last_sent_at, last_fire_key, last_error, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		name=excluded.name, enabled=excluded.enabled, env_ids=excluded.env_ids, tenant_id=excluded.tenant_id,
		instance_ids=excluded.instance_ids, db_names=excluded.db_names, cron_expr=excluded.cron_expr,
		recipients=excluded.recipients, top_n=excluded.top_n, updated_at=excluded.updated_at`,
		r.ID, r.Name, en, string(envIDs), r.TenantID, string(instIDs), string(dbNames), r.CronExpr, r.Recipients, r.TopN,
		r.LastSentAt, r.LastFireKey, r.LastError, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) DeleteSlowReport(id string) error {
	id = strings.TrimSpace(id)
	if _, err := d.Exec(`DELETE FROM tbl_cloud_slow_report_mails WHERE report_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_cloud_slow_reports WHERE id=?`, id)
	return err
}

func (d *DB) TouchSlowReportSend(id, fireKey, sentAt, errMsg string) error {
	_, err := d.Exec(`UPDATE tbl_cloud_slow_reports SET last_sent_at=?,
		last_fire_key=CASE WHEN ?='' THEN last_fire_key ELSE ? END,
		last_error=?, updated_at=? WHERE id=?`,
		sentAt, fireKey, fireKey, errMsg, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (d *DB) InsertSlowReportMail(m *SlowReportMail) error {
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	if m.SentAt == "" {
		m.SentAt = time.Now().UTC().Format(time.RFC3339)
	}
	ok := 0
	if m.OK {
		ok = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_cloud_slow_report_mails(
		id, report_id, sent_at, trigger_kind, env_label, tenant_label, recipients, subject, html, ok, error)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.ReportID, m.SentAt, m.Trigger, m.EnvLabel, m.TenantLabel, m.Recipients, m.Subject, m.HTML, ok, m.Error)
	return err
}

func (d *DB) ListSlowReportMails(envLabel string, limit int) ([]SlowReportMail, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, report_id, sent_at, trigger_kind, env_label, tenant_label, recipients, subject, ok, error
		FROM tbl_cloud_slow_report_mails`
	args := []any{}
	if strings.TrimSpace(envLabel) != "" {
		q += ` WHERE env_label LIKE ?`
		args = append(args, "%"+strings.TrimSpace(envLabel)+"%")
	}
	q += ` ORDER BY sent_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SlowReportMail{}
	for rows.Next() {
		var m SlowReportMail
		var ok int
		if err := rows.Scan(&m.ID, &m.ReportID, &m.SentAt, &m.Trigger, &m.EnvLabel, &m.TenantLabel, &m.Recipients, &m.Subject, &ok, &m.Error); err != nil {
			return nil, err
		}
		m.OK = ok != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) GetSlowReportMail(id string) (*SlowReportMail, error) {
	row := d.QueryRow(`SELECT id, report_id, sent_at, trigger_kind, env_label, tenant_label, recipients, subject, html, ok, error
		FROM tbl_cloud_slow_report_mails WHERE id=?`, strings.TrimSpace(id))
	var m SlowReportMail
	var ok int
	if err := row.Scan(&m.ID, &m.ReportID, &m.SentAt, &m.Trigger, &m.EnvLabel, &m.TenantLabel, &m.Recipients, &m.Subject, &m.HTML, &ok, &m.Error); err != nil {
		return nil, err
	}
	m.OK = ok != 0
	return &m, nil
}

func (d *DB) PurgeSlowReportMailsBefore(before string) error {
	_, err := d.Exec(`DELETE FROM tbl_cloud_slow_report_mails WHERE sent_at<>'' AND sent_at<?`, before)
	return err
}

// SlowReportWindow 汇总一个时间窗内命中的慢 SQL。
type SlowReportWindow struct {
	Count        int
	P99MS        float64
	Fingerprints map[string]struct{}
}

// SlowReportRank 一条指纹排名。
type SlowReportRank struct {
	Fingerprint string
	DBName      string
	SQL         string
	Calls       int
	AvgMS       float64
	P99MS       float64
	TotalMS     float64
	Raw         string
}

func (d *DB) SlowReportSummary(cloudIDs, dbNames []string, from, to time.Time) (SlowReportWindow, error) {
	var out SlowReportWindow
	out.Fingerprints = map[string]struct{}{}
	q, args, ok := slowReportWhere(cloudIDs, dbNames, from, to)
	if !ok {
		return out, nil
	}
	row := d.QueryRow(`SELECT COUNT(*), COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY duration_ms), 0)::float8 FROM tbl_cloud_slow_logs `+q, args...)
	if err := row.Scan(&out.Count, &out.P99MS); err != nil {
		return out, err
	}
	rows, err := d.Query(`SELECT DISTINCT fingerprint FROM tbl_cloud_slow_logs `+q, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return out, err
		}
		if fp != "" {
			out.Fingerprints[fp] = struct{}{}
		}
	}
	return out, rows.Err()
}

func (d *DB) SlowReportRanks(cloudIDs, dbNames []string, from, to time.Time, limit int) ([]SlowReportRank, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q, args, ok := slowReportWhere(cloudIDs, dbNames, from, to)
	if !ok {
		return []SlowReportRank{}, nil
	}
	args = append(args, limit)
	rows, err := d.Query(`SELECT fingerprint, COALESCE(db_name,''), MAX(sql_text), COUNT(*), COALESCE(AVG(duration_ms),0)::float8,
		COALESCE(percentile_cont(0.99) WITHIN GROUP (ORDER BY duration_ms), 0)::float8,
		COALESCE(SUM(duration_ms),0)::float8, COALESCE(MAX(raw_json),'')
		FROM tbl_cloud_slow_logs `+q+` GROUP BY fingerprint, COALESCE(db_name,'') ORDER BY SUM(duration_ms) DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SlowReportRank{}
	for rows.Next() {
		var r SlowReportRank
		if err := rows.Scan(&r.Fingerprint, &r.DBName, &r.SQL, &r.Calls, &r.AvgMS, &r.P99MS, &r.TotalMS, &r.Raw); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) DistinctSlowLogDBNames(cloudIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	ids := compactStrings(cloudIDs)
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(ids))
	q := `SELECT cloud_instance_id, db_name FROM tbl_cloud_slow_logs WHERE cloud_instance_id IN (?` + strings.Repeat(",?", len(ids)-1) + `) AND TRIM(db_name)<>'' GROUP BY cloud_instance_id, db_name`
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cloudID, name string
		if err := rows.Scan(&cloudID, &name); err != nil {
			return nil, err
		}
		out[cloudID] = append(out[cloudID], name)
	}
	return out, rows.Err()
}

func slowReportWhere(cloudIDs, dbNames []string, from, to time.Time) (string, []any, bool) {
	ids := compactStrings(cloudIDs)
	if len(ids) == 0 {
		return "", nil, false
	}
	args := make([]any, 0, len(ids)+len(dbNames)+2)
	q := `WHERE cloud_instance_id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
	for _, id := range ids {
		args = append(args, id)
	}
	names := compactStrings(dbNames)
	if len(names) > 0 {
		q += ` AND lower(db_name) IN (?` + strings.Repeat(",?", len(names)-1) + `)`
		for _, n := range names {
			args = append(args, strings.ToLower(n))
		}
	}
	q += ` AND log_time>=? AND log_time<?`
	args = append(args, from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	return q, args, true
}
