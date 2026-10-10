package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const migTaskCols = `id, name, status, source_instance_id, target_instance_id, source_db, target_db, description,
	created_at, updated_at, created_by, db_type, env, source_host, source_port, target_host, target_port,
	migration_mode, schema_strategy, conflict_strategy, transfer_mode, start_mode, scheduled_at,
	approval_status, assigned_approver, approved_by, approval_comment, batch_size, speed_limit,
	total_objects, completed_objects, total_rows, completed_rows, error_message, started_at, finished_at, issue_id`

func scanMigration(s interface{ Scan(...any) error }, t *MigrationTask) error {
	return s.Scan(
		&t.ID, &t.Name, &t.Status, &t.SourceID, &t.TargetID, &t.SourceDB, &t.TargetDB, &t.Description,
		&t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.DBType, &t.Env, &t.SourceHost, &t.SourcePort, &t.TargetHost, &t.TargetPort,
		&t.MigrationMode, &t.SchemaStrategy, &t.ConflictStrategy, &t.TransferMode, &t.StartMode, &t.ScheduledAt,
		&t.ApprovalStatus, &t.AssignedApprover, &t.ApprovedBy, &t.ApprovalComment, &t.BatchSize, &t.SpeedLimit,
		&t.TotalObjects, &t.CompletedObjects, &t.TotalRows, &t.CompletedRows, &t.ErrorMessage, &t.StartedAt, &t.FinishedAt, &t.IssueID,
	)
}

func (d *DB) CreateMigration(t *MigrationTask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = StatusPending
	}
	if t.MigrationMode == "" {
		t.MigrationMode = "full_data"
	}
	if t.SchemaStrategy == "" {
		t.SchemaStrategy = "init"
	}
	if t.ConflictStrategy == "" {
		t.ConflictStrategy = "abort"
	}
	if t.TransferMode == "" {
		t.TransferMode = "direct"
	}
	if t.StartMode == "" {
		t.StartMode = "immediate"
	}
	if t.ApprovalStatus == "" {
		t.ApprovalStatus = "approved"
	}
	if t.BatchSize <= 0 {
		t.BatchSize = 1000
	}
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_migration_tasks(`+migTaskCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.Status, t.SourceID, t.TargetID, t.SourceDB, t.TargetDB, t.Description,
		t.CreatedAt, t.UpdatedAt, t.CreatedBy, t.DBType, t.Env, t.SourceHost, t.SourcePort, t.TargetHost, t.TargetPort,
		t.MigrationMode, t.SchemaStrategy, t.ConflictStrategy, t.TransferMode, t.StartMode, t.ScheduledAt,
		t.ApprovalStatus, t.AssignedApprover, t.ApprovedBy, t.ApprovalComment, t.BatchSize, t.SpeedLimit,
		t.TotalObjects, t.CompletedObjects, t.TotalRows, t.CompletedRows, t.ErrorMessage, t.StartedAt, t.FinishedAt, t.IssueID,
	)
	if err != nil {
		return err
	}
	return d.AssignTenantID("tbl_migration_tasks", t.ID, d.inheritTenantFromInstance(t.SourceID))
}

func (d *DB) ReplaceMigrationObjects(taskID string, objs []MigrationObject) error {
	if _, err := d.Exec(`DELETE FROM tbl_migration_objects WHERE task_id=?`, taskID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	const batch = 500
	for start := 0; start < len(objs); start += batch {
		end := start + batch
		if end > len(objs) {
			end = len(objs)
		}
		chunk := objs[start:end]
		var b strings.Builder
		args := make([]any, 0, len(chunk)*17)
		b.WriteString(`INSERT INTO tbl_migration_objects(id, task_id, source_db_name, source_schema, source_table_name, target_db_name, target_schema, target_table_name, object_type, status, total_rows, completed_rows, total_bytes, completed_bytes, error_message, created_at, updated_at) VALUES `)
		for i := range chunk {
			o := &chunk[i]
			if o.ID == "" {
				o.ID = uuid.NewString()
			}
			o.TaskID = taskID
			if o.Status == "" {
				o.Status = StatusPending
			}
			if o.ObjectType == "" {
				o.ObjectType = "table"
			}
			o.CreatedAt, o.UpdatedAt = now, now
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
			args = append(args, o.ID, o.TaskID, o.SourceDB, o.SourceSchema, o.SourceTable, o.TargetDB, o.TargetSchema, o.TargetTable, o.ObjectType, o.Status, o.TotalRows, o.CompletedRows, o.TotalBytes, o.CompletedBytes, o.ErrorMessage, o.CreatedAt, o.UpdatedAt)
		}
		if _, err := d.Exec(b.String(), args...); err != nil {
			return err
		}
	}
	_, err := d.Exec(`UPDATE tbl_migration_tasks SET total_objects=?, updated_at=? WHERE id=?`, len(objs), now, taskID)
	return err
}

func (d *DB) ListMigrations() ([]MigrationTask, error) {
	return d.ListMigrationsInTenant("")
}

func (d *DB) ListMigrationsInTenant(tenantID string) ([]MigrationTask, error) {
	q := `SELECT ` + migTaskCols + ` FROM tbl_migration_tasks WHERE 1=1`
	args := []any{}
	q, args = appendTenantFilter(q, args, tenantID)
	q += ` ORDER BY created_at DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationTask
	for rows.Next() {
		var t MigrationTask
		if err := scanMigration(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) GetMigrationByIssueID(issueID string) (*MigrationTask, error) {
	if strings.TrimSpace(issueID) == "" {
		return nil, nil
	}
	t := &MigrationTask{}
	err := scanMigration(d.QueryRow(`SELECT `+migTaskCols+` FROM tbl_migration_tasks WHERE issue_id=? ORDER BY created_at DESC LIMIT 1`, issueID), t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func MigrationApproved(t *MigrationTask) bool {
	if t == nil {
		return false
	}
	switch t.ApprovalStatus {
	case StatusPending, "draft", StatusRejected, StatusCanceled:
		return false
	}
	if (t.ApprovalStatus == "" || t.ApprovalStatus == "none") && t.IssueID == "" {
		return true
	}
	return t.ApprovalStatus == StatusApproved
}

func (d *DB) GetMigration(id string) (*MigrationTask, error) {
	t := &MigrationTask{}
	err := scanMigration(d.QueryRow(`SELECT `+migTaskCols+` FROM tbl_migration_tasks WHERE id=?`, id), t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Objects, _ = d.ListMigrationObjects(id)
	t.Prechecks, _ = d.ListMigrationPrechecks(id)
	return t, nil
}

func (d *DB) ListMigrationObjects(taskID string) ([]MigrationObject, error) {
	rows, err := d.Query(`SELECT id, task_id, source_db_name, source_schema, source_table_name, target_db_name, target_schema, target_table_name, object_type, status, total_rows, completed_rows, COALESCE(total_bytes,0), COALESCE(completed_bytes,0), error_message, created_at, updated_at FROM tbl_migration_objects WHERE task_id=? ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationObject
	for rows.Next() {
		var o MigrationObject
		if err := rows.Scan(&o.ID, &o.TaskID, &o.SourceDB, &o.SourceSchema, &o.SourceTable, &o.TargetDB, &o.TargetSchema, &o.TargetTable, &o.ObjectType, &o.Status, &o.TotalRows, &o.CompletedRows, &o.TotalBytes, &o.CompletedBytes, &o.ErrorMessage, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (d *DB) UpdateMigrationStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_migration_tasks SET status=?, updated_at=? WHERE id=?`, status, now, id)
	return err
}

func (d *DB) UpdateMigration(t *MigrationTask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	t.UpdatedAt = now
	_, err := d.Exec(
		`UPDATE tbl_migration_tasks SET name=?, status=?, source_instance_id=?, target_instance_id=?, source_db=?, target_db=?, description=?,
			updated_at=?, created_by=?, db_type=?, env=?, source_host=?, source_port=?, target_host=?, target_port=?,
			migration_mode=?, schema_strategy=?, conflict_strategy=?, transfer_mode=?, start_mode=?, scheduled_at=?,
			approval_status=?, assigned_approver=?, approved_by=?, approval_comment=?, batch_size=?, speed_limit=?,
			total_objects=?, completed_objects=?, total_rows=?, completed_rows=?, error_message=?, started_at=?, finished_at=?, issue_id=?
		 WHERE id=?`,
		t.Name, t.Status, t.SourceID, t.TargetID, t.SourceDB, t.TargetDB, t.Description,
		t.UpdatedAt, t.CreatedBy, t.DBType, t.Env, t.SourceHost, t.SourcePort, t.TargetHost, t.TargetPort,
		t.MigrationMode, t.SchemaStrategy, t.ConflictStrategy, t.TransferMode, t.StartMode, t.ScheduledAt,
		t.ApprovalStatus, t.AssignedApprover, t.ApprovedBy, t.ApprovalComment, t.BatchSize, t.SpeedLimit,
		t.TotalObjects, t.CompletedObjects, t.TotalRows, t.CompletedRows, t.ErrorMessage, t.StartedAt, t.FinishedAt, t.IssueID, t.ID,
	)
	return err
}

func (d *DB) UpdateMigrationObject(o *MigrationObject) error {
	now := time.Now().UTC().Format(time.RFC3339)
	o.UpdatedAt = now
	_, err := d.Exec(
		`UPDATE tbl_migration_objects SET status=?, total_rows=?, completed_rows=?, total_bytes=?, completed_bytes=?, error_message=?, updated_at=? WHERE id=?`,
		o.Status, o.TotalRows, o.CompletedRows, o.TotalBytes, o.CompletedBytes, o.ErrorMessage, o.UpdatedAt, o.ID,
	)
	return err
}

func (d *DB) AddMigrationLog(taskID, level, message string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO tbl_migration_logs(task_id, log_level, message, created_at) VALUES(?,?,?,?)`, taskID, level, message, now)
	return err
}

func (d *DB) ListMigrationLogs(taskID string, sinceID int64, limit int) ([]MigrationLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := d.Query(`SELECT id, task_id, log_level, message, created_at FROM tbl_migration_logs WHERE task_id=? AND id>? ORDER BY id LIMIT ?`, taskID, sinceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationLog
	for rows.Next() {
		var l MigrationLog
		if err := rows.Scan(&l.ID, &l.TaskID, &l.Level, &l.Message, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.LogType = "task"
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *DB) ReplaceMigrationPrechecks(taskID string, items []MigrationPrecheck) error {
	if _, err := d.Exec(`DELETE FROM tbl_migration_prechecks WHERE task_id=?`, taskID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, it := range items {
		if _, err := d.Exec(
			`INSERT INTO tbl_migration_prechecks(task_id, check_item, check_level, check_status, message, created_at) VALUES(?,?,?,?,?,?)`,
			taskID, it.Item, it.Level, it.Status, it.Message, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ListMigrationPrechecks(taskID string) ([]MigrationPrecheck, error) {
	rows, err := d.Query(`SELECT id, task_id, check_item, check_level, check_status, message FROM tbl_migration_prechecks WHERE task_id=? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationPrecheck
	for rows.Next() {
		var p MigrationPrecheck
		if err := rows.Scan(&p.ID, &p.TaskID, &p.Item, &p.Level, &p.Status, &p.Message); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) ListDueScheduledMigrations() ([]MigrationTask, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := d.Query(`SELECT `+migTaskCols+` FROM tbl_migration_tasks WHERE status=? AND scheduled_at<>'' AND scheduled_at<=?`, StatusScheduled, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MigrationTask
	for rows.Next() {
		var t MigrationTask
		if err := scanMigration(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) DeleteMigration(id string) error {
	if _, err := d.Exec(`DELETE FROM tbl_migration_prechecks WHERE task_id=?`, id); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM tbl_migration_logs WHERE task_id=?`, id); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM tbl_migration_objects WHERE task_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_migration_tasks WHERE id=?`, id)
	return err
}

func NormalizeDBType(t string) string {
	n := strings.ToLower(strings.TrimSpace(t))
	if n == "postgresql" || n == "pg" || n == "pgsql" {
		return "postgres"
	}
	return n
}

func (d *DB) GetMigrationTags(id string) map[string]any {
	out := map[string]any{}
	var raw string
	err := d.QueryRow(`SELECT COALESCE(tags,'{}') FROM tbl_migration_tasks WHERE id=?`, id).Scan(&raw)
	if err != nil || strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func (d *DB) SetMigrationTags(id string, tags map[string]any) error {
	if tags == nil {
		tags = map[string]any{}
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`UPDATE tbl_migration_tasks SET tags=?, updated_at=? WHERE id=?`, string(b), now, id)
	return err
}

func MergeMigrationTags(cur map[string]any, patch map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range cur {
		out[k] = v
	}
	for k, v := range patch {
		if k == "selection" {
			sel := map[string]any{}
			if old, ok := out["selection"].(map[string]any); ok {
				for sk, sv := range old {
					sel[sk] = sv
				}
			}
			if next, ok := v.(map[string]any); ok {
				for sk, sv := range next {
					sel[sk] = sv
				}
			}
			out["selection"] = sel
			continue
		}
		out[k] = v
	}
	return out
}
