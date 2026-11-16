package store

import (
	"time"

	"github.com/google/uuid"
)

// AnalysisKindHostDisk 是主机磁盘增长分析。后续其它分析复用本表，只换 kind。
const AnalysisKindHostDisk = "host_disk"

// AnalysisKindPGStorage 是 PostgreSQL 总览智能分析：库文件增长，以及 QPS/TPS、锁和 CPU。
const AnalysisKindPGStorage = "pg_storage"

// AnalysisReport 是一条已保存的分析报告。Scope 和 Payload 是 JSON 文本，不按项目拆列。
type AnalysisReport struct {
	ID        string
	TenantID  string
	Kind      string
	UserID    string
	Username  string
	Title     string
	Summary   string
	Scope     string
	Payload   string
	CreatedAt string
}

// AnalysisReportListItem 是历史列表，不含全文。
type AnalysisReportListItem struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	Scope     string `json:"scope"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"`
}

func (d *DB) ensureAnalysisReportsTable() error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_analysis_reports (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT '',
		user_id TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT '',
		payload TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return err
	}
	_, err := d.Exec(`CREATE INDEX IF NOT EXISTS idx_analysis_reports_tenant_kind ON tbl_analysis_reports(tenant_id, kind, created_at DESC)`)
	return err
}

// InsertAnalysisReport 写入一条报告并返回 id。
func (d *DB) InsertAnalysisReport(r *AnalysisReport) (string, error) {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := d.Exec(`INSERT INTO tbl_analysis_reports(
		id, tenant_id, kind, user_id, username, title, summary, scope, payload, created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.TenantID, r.Kind, r.UserID, r.Username, r.Title, r.Summary, r.Scope, r.Payload, r.CreatedAt)
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

// ListAnalysisReports 返回本租户某 kind 最近的报告，不含 payload。
func (d *DB) ListAnalysisReports(tenantID, kind string, limit int) ([]AnalysisReportListItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	rows, err := d.Query(`SELECT id, kind, title, summary, scope, username, created_at
		FROM tbl_analysis_reports WHERE tenant_id=? AND kind=? ORDER BY created_at DESC LIMIT ?`,
		tenantID, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnalysisReportListItem
	for rows.Next() {
		var it AnalysisReportListItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.Title, &it.Summary, &it.Scope, &it.Username, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetAnalysisReport 只返回本租户的报告。不存在时 sql.ErrNoRows。
func (d *DB) GetAnalysisReport(tenantID, id string) (*AnalysisReport, error) {
	var r AnalysisReport
	err := d.QueryRow(`SELECT id, tenant_id, kind, user_id, username, title, summary, scope, payload, created_at
		FROM tbl_analysis_reports WHERE id=? AND tenant_id=?`, id, tenantID).
		Scan(&r.ID, &r.TenantID, &r.Kind, &r.UserID, &r.Username, &r.Title, &r.Summary, &r.Scope, &r.Payload, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
