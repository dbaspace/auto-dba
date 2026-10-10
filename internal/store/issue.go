package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func ValidIssueType(t string) bool {
	switch t {
	case IssueTypeDDL, IssueTypeDML, IssueTypeDCL, IssueTypeDQL, IssueTypeKafka,
		IssueTypeCDC, IssueTypeArchive, IssueTypeFlashback, IssueTypeMigration, IssueTypeMullang, IssueTypeCreateDB,
		IssueTypeHA, IssueTypeRedis, IssueTypeBackupRestore, IssueTypeSrCluster, IssueTypeDbDeploy:
		return true
	default:
		return false
	}
}

func (d *DB) CreateIssue(it *Issue) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.TicketNo == "" {
		it.TicketNo = fmt.Sprintf("T%s", strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	}
	if it.Status == "" {
		it.Status = StatusPending
	}
	it.CreatedAt, it.UpdatedAt = now, now
	if strings.TrimSpace(it.TenantID) == "" {
		it.TenantID = d.inheritTenantFromInstance(it.InstanceID)
	}
	if strings.TrimSpace(it.TenantID) == "" {
		return errors.New("tenant_id 必填")
	}
	_, err := d.Exec(
		`INSERT INTO tbl_issues(id, ticket_no, issue_type, instance_id, database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.TicketNo, it.IssueType, it.InstanceID, it.DatabaseID, it.DBName, it.SQLContent, it.Description,
		it.Status, it.Submitter, it.AssignedTo, it.RejectReason, it.NeedRollback, it.ScheduledAt, it.CreatedAt, it.UpdatedAt,
	)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_issues", it.ID, it.TenantID)
}

func (d *DB) GetIssue(id string) (*Issue, error) {
	it := &Issue{}
	var exec sql.NullString
	var hist sql.NullString
	err := d.QueryRow(
		`SELECT id, ticket_no, issue_type, instance_id, database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, execute_result, COALESCE(assignee_transfer_history,'[]'), created_at, updated_at
		 FROM tbl_issues WHERE id=?`, id,
	).Scan(
		&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
		&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &exec, &hist, &it.CreatedAt, &it.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	it.ExecuteResult = exec.String
	it.AssigneeTransferHistory = hist.String
	if it.AssigneeTransferHistory == "" {
		it.AssigneeTransferHistory = "[]"
	}
	it.Type = it.IssueType
	attachIssueExec(it)
	return it, nil
}

func (d *DB) GetIssueInTenant(id, tenantID string) (*Issue, error) {
	it, err := d.GetIssue(id)
	if err != nil || it == nil {
		return it, err
	}
	ok, err := d.rowVisibleInTenant("tbl_issues", id, tenantID)
	if err != nil || !ok {
		return nil, err
	}
	it.TenantID = strings.TrimSpace(tenantID)
	return it, nil
}

func (d *DB) ListIssues(issueType, status, q, viewer string, viewAll bool) ([]Issue, error) {
	return d.ListIssuesInTenant("", issueType, status, q, viewer, viewAll)
}

func (d *DB) ListIssuesInTenant(tenantID, issueType, status, q, viewer string, viewAll bool) ([]Issue, error) {
	query := `SELECT id, ticket_no, issue_type, instance_id, database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, created_at, updated_at FROM tbl_issues WHERE 1=1`
	var args []any
	if tid := strings.TrimSpace(tenantID); tid != "" {
		query += ` AND COALESCE(tenant_id,'')=?`
		args = append(args, tid)
	}
	if issueType != "" {
		query += ` AND issue_type=?`
		args = append(args, issueType)
	} else if d.BoolArg("ticket_exclude_cdc_when_empty", true) {
		query += ` AND issue_type<>?`
		args = append(args, IssueTypeCDC)
	}
	if status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}
	if q != "" {
		like := "%" + q + "%"
		query += ` AND (ticket_no LIKE ? OR db_name LIKE ? OR description LIKE ?)`
		args = append(args, like, like, like)
	}
	if !viewAll && viewer != "" {
		query += ` AND (submitter=? OR assigned_to=?)`
		args = append(args, viewer, viewer)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var it Issue
		if err := rows.Scan(
			&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
			&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			return nil, err
		}
		it.Type = it.IssueType
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) UpdateIssueStatus(id, status, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_issues SET status=?, reject_reason=?, updated_at=? WHERE id=?`, status, reason, now, id)
	return err
}

// ListStalePendingIssues 返回仍待审批且创建时间早于 before 的工单。
func (d *DB) ListStalePendingIssues(before time.Time) ([]Issue, error) {
	cutoff := before.UTC().Format(time.RFC3339)
	rows, err := d.Query(
		`SELECT id, ticket_no, issue_type, instance_id, database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, created_at, updated_at
		 FROM tbl_issues WHERE status=? AND created_at<? ORDER BY created_at ASC`,
		StatusPending, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var it Issue
		if err := rows.Scan(
			&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
			&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			return nil, err
		}
		it.Type = it.IssueType
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) UpdateIssueExec(id, status, resultJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_issues SET status=?, execute_result=?, updated_at=? WHERE id=?`, status, resultJSON, now, id)
	return err
}

func attachIssueExec(it *Issue) {
	if it == nil {
		return
	}
	raw := strings.TrimSpace(it.ExecuteResult)
	if raw == "" {
		return
	}
	var obj any
	if json.Unmarshal([]byte(raw), &obj) == nil {
		it.Exec = obj
	}
}

func (d *DB) IssueStats() (map[string]int, error) {
	rows, err := d.Query(`SELECT status, COUNT(1) FROM tbl_issues GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}
