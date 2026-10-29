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
		IssueTypeHA, IssueTypeRedis, IssueTypeBackupRestore, IssueTypeSrCluster, IssueTypeDbDeploy,
		IssueTypeResourceApply, IssueTypeK8sChange, IssueTypeDnsChange,
		IssueTypeCloudPg, IssueTypeCloudMysql, IssueTypeCloudRedis, IssueTypeCloudHost:
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
		`INSERT INTO tbl_issues(id, ticket_no, issue_type, instance_id, domain, database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.TicketNo, it.IssueType, it.InstanceID, it.Domain, it.DatabaseID, it.DBName, it.SQLContent, it.Description,
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
	var tid sql.NullString
	var prog sql.NullString
	err := d.QueryRow(
		`SELECT id, ticket_no, issue_type, instance_id, COALESCE(domain,''), database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, execute_result, COALESCE(assignee_transfer_history,'[]'), created_at, updated_at, COALESCE(tenant_id,''), COALESCE(exec_progress,'')
		 FROM tbl_issues WHERE id=?`, id,
	).Scan(
		&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.Domain, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
		&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &exec, &hist, &it.CreatedAt, &it.UpdatedAt, &tid, &prog,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	it.ExecuteResult = exec.String
	it.ExecProgressRaw = prog.String
	it.ExecProgress = ParseExecProgress(prog.String)
	it.AssigneeTransferHistory = hist.String
	if it.AssigneeTransferHistory == "" {
		it.AssigneeTransferHistory = "[]"
	}
	it.TenantID = strings.TrimSpace(tid.String)
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
	if strings.TrimSpace(it.TenantID) == "" {
		it.TenantID = strings.TrimSpace(tenantID)
	}
	return it, nil
}

func (d *DB) ListIssues(issueType, status, q, viewer string, viewAll bool) ([]Issue, error) {
	return d.ListIssuesInTenant("", issueType, status, q, viewer, viewAll)
}

func (d *DB) ListIssuesInTenant(tenantID, issueType, status, q, viewer string, viewAll bool) ([]Issue, error) {
	query := `SELECT id, ticket_no, issue_type, instance_id, COALESCE(domain,''), database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, created_at, updated_at, COALESCE(tenant_id,''), COALESCE(exec_progress,'') FROM tbl_issues WHERE 1=1`
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
		var tid string
		var prog sql.NullString
		if err := rows.Scan(
			&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.Domain, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
			&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &it.CreatedAt, &it.UpdatedAt, &tid, &prog,
		); err != nil {
			return nil, err
		}
		it.TenantID = strings.TrimSpace(tid)
		it.Type = it.IssueType
		it.ExecProgressRaw = prog.String
		it.ExecProgress = ParseExecProgress(prog.String)
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListResourceApplyAcrossTenants 全平台平台资源申请（不按 tenant_id 过滤），供平台管理员/白名单审核。
func (d *DB) ListResourceApplyAcrossTenants(status, q string) ([]Issue, error) {
	return d.ListIssuesInTenant("", IssueTypeResourceApply, status, q, "", true)
}

// ListResourceApplyAssignedToAcrossTenants 全平台「指派给指定用户」的平台资源申请（仅 resource_apply，不按 tenant_id）。
// 供 dba 等跨租审批人在本租上下文下列表/待办发现指派给自己的单；不影响其它工单类型。
func (d *DB) ListResourceApplyAssignedToAcrossTenants(assignee, status, q string) ([]Issue, error) {
	return d.listIssuesAssignedToAcrossTenants(assignee, IssueTypeResourceApply, status, q, true)
}

// ListIssuesAssignedToAcrossTenants 全平台指派给指定用户的工单（可按类型过滤；默认排除 resource_apply，避免与专用合并重复）。
func (d *DB) ListIssuesAssignedToAcrossTenants(assignee, issueType, status, q string) ([]Issue, error) {
	return d.listIssuesAssignedToAcrossTenants(assignee, issueType, status, q, false)
}

func (d *DB) listIssuesAssignedToAcrossTenants(assignee, issueType, status, q string, onlyResourceApply bool) ([]Issue, error) {
	assignee = strings.TrimSpace(assignee)
	if assignee == "" {
		return []Issue{}, nil
	}
	query := `SELECT id, ticket_no, issue_type, instance_id, COALESCE(domain,''), database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, created_at, updated_at, COALESCE(tenant_id,''), COALESCE(exec_progress,'')
		 FROM tbl_issues WHERE LOWER(assigned_to)=LOWER(?)`
	args := []any{assignee}
	if onlyResourceApply {
		query += ` AND issue_type=?`
		args = append(args, IssueTypeResourceApply)
	} else if t := strings.TrimSpace(issueType); t != "" {
		query += ` AND issue_type=?`
		args = append(args, t)
	} else {
		query += ` AND issue_type<>?`
		args = append(args, IssueTypeResourceApply)
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
	query += ` ORDER BY created_at DESC`
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var it Issue
		var tid string
		var prog sql.NullString
		if err := rows.Scan(
			&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.Domain, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
			&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &it.CreatedAt, &it.UpdatedAt, &tid, &prog,
		); err != nil {
			return nil, err
		}
		it.TenantID = strings.TrimSpace(tid)
		it.Type = it.IssueType
		it.ExecProgressRaw = prog.String
		it.ExecProgress = ParseExecProgress(prog.String)
		out = append(out, it)
	}
	if out == nil {
		out = []Issue{}
	}
	return out, rows.Err()
}

func (d *DB) UpdateIssueStatus(id, status, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_issues SET status=?, reject_reason=?, updated_at=? WHERE id=?`, status, reason, now, id)
	return err
}

// ListRunningAutoExecIssueIDs 返回仍在后台执行的 DDL/DML/建库工单。
func (d *DB) ListRunningAutoExecIssueIDs() ([]string, error) {
	rows, err := d.Query(
		`SELECT id FROM tbl_issues WHERE status=? AND issue_type IN (?,?,?) ORDER BY updated_at ASC`,
		StatusRunning, IssueTypeDDL, IssueTypeDML, IssueTypeCreateDB,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if strings.TrimSpace(id) != "" {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// ListStalePendingIssues 返回仍待审批且创建时间早于 before 的工单。
func (d *DB) ListStalePendingIssues(before time.Time) ([]Issue, error) {
	cutoff := before.UTC().Format(time.RFC3339)
	rows, err := d.Query(
		`SELECT id, ticket_no, issue_type, instance_id, COALESCE(domain,''), database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, created_at, updated_at
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
			&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.Domain, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
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

// UpdateIssueExecRunning 只在仍为执行中时写入本轮结果，避免终止后被进度检查点改回 running。
func (d *DB) UpdateIssueExecRunning(id, resultJSON string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := d.Exec(
		`UPDATE tbl_issues SET execute_result=?, updated_at=? WHERE id=? AND status=?`,
		resultJSON, now, id, StatusRunning,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (d *DB) SaveIssueProgress(id string, p *ExecProgress) error {
	if d == nil {
		return nil
	}
	raw := ""
	if p != nil {
		b, err := json.Marshal(p)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_issues SET exec_progress=?, updated_at=? WHERE id=?`, raw, now, id)
	return err
}

func (d *DB) ClearIssueProgress(id string) error {
	if d == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_issues SET exec_progress='', updated_at=? WHERE id=?`, now, id)
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
