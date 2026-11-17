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
		IssueTypeCloudPg, IssueTypeCloudMysql, IssueTypeCloudRedis, IssueTypeCloudKafka, IssueTypeCloudHost:
		return true
	default:
		return false
	}
}

func (d *DB) CreateIssue(it *Issue) error {
	if it == nil {
		return errors.New("issue 必填")
	}
	if err := d.CheckTicketSubmitAllowed(it.Submitter); err != nil {
		return err
	}
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

// issueSummaryFilter 工单列表摘要的筛选。不读取 sql_content。
type issueSummaryFilter struct {
	TenantID             string
	IssueType            string
	Status               string
	Q                    string
	Viewer               string
	ViewAll              bool
	Assignee             string
	ExcludeCDC           bool
	ExcludeResourceApply bool
}

// ListIssueSummariesInTenant 工单列表用，不含 SQL 正文。环境、库类型来自关联实例。
func (d *DB) ListIssueSummariesInTenant(tenantID, issueType, status, q, viewer string, viewAll bool) ([]Issue, error) {
	return d.listIssueSummaries(issueSummaryFilter{
		TenantID: tenantID, IssueType: issueType, Status: status, Q: q, Viewer: viewer, ViewAll: viewAll,
		ExcludeCDC: true,
	})
}

// ListResourceApplySummariesAcrossTenants 全平台平台资源申请摘要。
func (d *DB) ListResourceApplySummariesAcrossTenants(status, q string) ([]Issue, error) {
	return d.listIssueSummaries(issueSummaryFilter{
		IssueType: IssueTypeResourceApply, Status: status, Q: q, ViewAll: true,
	})
}

// ListIssueSummariesAssignedToAcrossTenants 全平台指派给指定用户的工单摘要。
func (d *DB) ListIssueSummariesAssignedToAcrossTenants(assignee, issueType, status, q string) ([]Issue, error) {
	if strings.TrimSpace(assignee) == "" {
		return []Issue{}, nil
	}
	return d.listIssueSummaries(issueSummaryFilter{
		Assignee: assignee, IssueType: issueType, Status: status, Q: q, ViewAll: true,
		ExcludeResourceApply: strings.TrimSpace(issueType) == "",
	})
}

func (d *DB) listIssueSummaries(f issueSummaryFilter) ([]Issue, error) {
	query := `SELECT i.id, i.ticket_no, i.issue_type, i.instance_id, COALESCE(i.domain,''), i.database_id, i.db_name, i.description, i.status, i.submitter, i.assigned_to, i.reject_reason, i.need_rollback, i.scheduled_at, i.created_at, i.updated_at, COALESCE(i.tenant_id,''), COALESCE(i.exec_progress,''), COALESCE(inst.idc,''), COALESCE(inst.db_type,'')
		FROM tbl_issues i LEFT JOIN tbl_instances inst ON inst.id = i.instance_id WHERE 1=1`
	var args []any
	if assignee := strings.TrimSpace(f.Assignee); assignee != "" {
		query += ` AND LOWER(i.assigned_to)=LOWER(?)`
		args = append(args, assignee)
	}
	if tid := strings.TrimSpace(f.TenantID); tid != "" {
		query += ` AND i.tenant_id=?`
		args = append(args, tid)
	}
	if issueType := strings.TrimSpace(f.IssueType); issueType != "" {
		query += ` AND i.issue_type=?`
		args = append(args, issueType)
	} else if f.ExcludeResourceApply {
		query += ` AND i.issue_type<>?`
		args = append(args, IssueTypeResourceApply)
	} else if f.ExcludeCDC && d.BoolArg("ticket_exclude_cdc_when_empty", true) {
		query += ` AND i.issue_type<>?`
		args = append(args, IssueTypeCDC)
	}
	if status := strings.TrimSpace(f.Status); status != "" {
		query += ` AND i.status=?`
		args = append(args, status)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + q + "%"
		query += ` AND (i.ticket_no LIKE ? OR i.db_name LIKE ? OR i.description LIKE ?)`
		args = append(args, like, like, like)
	}
	if !f.ViewAll && strings.TrimSpace(f.Viewer) != "" && strings.TrimSpace(f.Assignee) == "" {
		query += ` AND (i.submitter=? OR i.assigned_to=?)`
		args = append(args, f.Viewer, f.Viewer)
	}
	query += ` ORDER BY i.created_at DESC`
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
			&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.Domain, &it.DatabaseID, &it.DBName, &it.Description,
			&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &it.CreatedAt, &it.UpdatedAt, &tid, &prog,
			&it.Env, &it.DBType,
		); err != nil {
			return nil, err
		}
		it.TenantID = strings.TrimSpace(tid)
		it.Env = strings.TrimSpace(it.Env)
		it.DBType = strings.TrimSpace(it.DBType)
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

// ListIssueSQLPrefixes 只取仍缺展示字段的工单正文前 4096 个字符。
func (d *DB) ListIssueSQLPrefixes(ids []string) (map[string]string, error) {
	out := map[string]string{}
	if d == nil || len(ids) == 0 {
		return out, nil
	}
	const chunk = 400
	for start := 0; start < len(ids); start += chunk {
		end := start + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[start:end]
		ph := make([]string, len(part))
		args := make([]any, len(part))
		for i, id := range part {
			ph[i] = "?"
			args[i] = id
		}
		q := `SELECT id, LEFT(COALESCE(sql_content,''), 4096) FROM tbl_issues WHERE id IN (` + strings.Join(ph, ",") + `) AND LEFT(COALESCE(sql_content,''), 1)='{'`
		rows, err := d.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, raw string
			if err := rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = raw
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
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

// SetIssueStatus 只改状态，保留驳回原因和执行结果。
func (d *DB) SetIssueStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_issues SET status=?, updated_at=? WHERE id=?`, status, now, id)
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
