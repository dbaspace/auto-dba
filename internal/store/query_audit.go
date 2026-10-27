package store

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxQueryAuditStatement = 8 * 1024

func TruncateQueryAuditStatement(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxQueryAuditStatement {
		return s
	}
	r := []rune(s)
	return string(r[:maxQueryAuditStatement]) + "…"
}

func (d *DB) CreateQueryAudit(it *QueryAudit) error {
	if it == nil {
		return nil
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.CreatedAt == "" {
		it.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	it.Kind = strings.TrimSpace(it.Kind)
	it.Username = strings.TrimSpace(it.Username)
	it.DisplayName = strings.TrimSpace(it.DisplayName)
	it.IP = strings.TrimSpace(it.IP)
	it.UserAgent = strings.TrimSpace(it.UserAgent)
	if len(it.UserAgent) > 512 {
		it.UserAgent = it.UserAgent[:512]
	}
	it.Statement = TruncateQueryAuditStatement(it.Statement)
	it.Message = TruncateQueryAuditStatement(it.Message)
	it.ClientType = strings.TrimSpace(it.ClientType)
	it.TokenID = strings.TrimSpace(it.TokenID)
	success := 0
	if it.Success {
		success = 1
	}
	_, err := d.Exec(
		`INSERT INTO tbl_query_audits(
			id, kind, user_id, username, display_name, ip, user_agent,
			instance_id, instance_name, db_type, target, statement,
			success, message, row_count, elapsed_ms, client_type, token_id, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.Kind, it.UserID, it.Username, it.DisplayName, it.IP, it.UserAgent,
		it.InstanceID, it.InstanceName, it.DBType, it.Target, it.Statement,
		success, it.Message, it.RowCount, it.ElapsedMS, it.ClientType, it.TokenID, it.CreatedAt,
	)
	return err
}

type QueryAuditFilter struct {
	Username   string
	Kind       string
	Success    *bool
	Q          string
	ClientType string
	DateFrom   string
	DateTo     string
	Page       int
	PageSize   int
}

func buildQueryAuditWhere(f QueryAuditFilter) (string, []any) {
	where := `WHERE 1=1`
	args := []any{}
	if s := strings.TrimSpace(f.Username); s != "" {
		where += ` AND username LIKE ?`
		args = append(args, "%"+s+"%")
	}
	if s := strings.ToLower(strings.TrimSpace(f.Kind)); s != "" {
		where += ` AND kind=?`
		args = append(args, s)
	}
	if f.Success != nil {
		v := 0
		if *f.Success {
			v = 1
		}
		where += ` AND success=?`
		args = append(args, v)
	}
	if s := strings.ToLower(strings.TrimSpace(f.ClientType)); s != "" {
		where += ` AND client_type=?`
		args = append(args, s)
	}
	if s := strings.TrimSpace(f.Q); s != "" {
		where += ` AND (statement LIKE ? OR target LIKE ? OR instance_name LIKE ?)`
		like := "%" + s + "%"
		args = append(args, like, like, like)
	}
	if s := strings.TrimSpace(f.DateFrom); s != "" {
		where += ` AND created_at>=?`
		args = append(args, s)
	}
	if s := strings.TrimSpace(f.DateTo); s != "" {
		where += ` AND created_at<=?`
		args = append(args, s)
	}
	return where, args
}

func (d *DB) ListQueryAudits(f QueryAuditFilter) ([]QueryAudit, int, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}
	where, args := buildQueryAuditWhere(f)
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_query_audits `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT id, kind, user_id, username, display_name, ip, user_agent,
		instance_id, instance_name, db_type, target, statement,
		success, message, row_count, elapsed_ms,
		COALESCE(client_type,''), COALESCE(token_id,''), created_at
		FROM tbl_query_audits ` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []QueryAudit{}
	for rows.Next() {
		var it QueryAudit
		var success int
		if err := rows.Scan(
			&it.ID, &it.Kind, &it.UserID, &it.Username, &it.DisplayName, &it.IP, &it.UserAgent,
			&it.InstanceID, &it.InstanceName, &it.DBType, &it.Target, &it.Statement,
			&success, &it.Message, &it.RowCount, &it.ElapsedMS, &it.ClientType, &it.TokenID, &it.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		it.Success = success != 0
		out = append(out, it)
	}
	return out, total, rows.Err()
}
