package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	GlobalArgArchiveSettings     = "archive_settings"
	GlobalArgArchiveMonitorRules = "archive_monitor_rules"
)

type ArchiveExtra struct {
	Strategy     string `json:"strategy,omitempty"`
	RunMode      string `json:"run_mode,omitempty"`
	ScheduleExpr string `json:"schedule_expr,omitempty"`
	TargetInstID string `json:"target_instance_id,omitempty"`
	TargetDB     string `json:"target_db,omitempty"`
	TargetTable  string `json:"target_table,omitempty"`
	OSSBucket    string `json:"oss_bucket,omitempty"`
	PGDeleteMode string `json:"pg_delete_mode,omitempty"`
	ScriptType   string `json:"script_type,omitempty"`
	DBType       string `json:"db_type,omitempty"`
	CleanupAfter bool   `json:"cleanup_after,omitempty"`
}

func ParseArchiveExtra(t *ArchiveTask) ArchiveExtra {
	out := ArchiveExtra{Strategy: ArchiveStrategy(t), RunMode: ArchiveRunMode(t)}
	if t == nil {
		return out
	}
	out.ScheduleExpr = strings.TrimSpace(t.ScheduleExpr)
	raw := t.Condition
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-- auto-dba-schedule:") {
			if out.ScheduleExpr == "" {
				out.ScheduleExpr = strings.TrimSpace(strings.TrimPrefix(line, "-- auto-dba-schedule:"))
			}
			continue
		}
		if strings.HasPrefix(line, "-- auto-dba-extra:") {
			var m map[string]any
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "-- auto-dba-extra:")), &m) == nil {
				out.TargetInstID = anyString(m["target_instance_id"])
				out.TargetDB = anyString(m["target_db"])
				out.TargetTable = anyString(m["target_table"])
				out.OSSBucket = anyString(m["oss_bucket"])
				out.PGDeleteMode = anyString(m["pg_delete_mode"])
				out.ScriptType = anyString(m["script_type"])
				out.DBType = anyString(m["db_type"])
				out.CleanupAfter = anyBool(m["cleanup_after"])
			}
		}
	}
	return out
}

func ArchiveConditionSQL(t *ArchiveTask) string {
	if t == nil {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(t.Condition, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "-- auto-dba-archive:") ||
			strings.HasPrefix(trim, "-- auto-dba-schedule:") ||
			strings.HasPrefix(trim, "-- auto-dba-extra:") {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
	}
	return ""
}

func anyBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case float64:
		return x != 0
	}
	return false
}

func (d *DB) CreateArchiveRun(r *ArchiveRun) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = now
	}
	if r.StartedAt == "" {
		r.StartedAt = now
	}
	if r.Status == "" {
		r.Status = StatusRunning
	}
	_, err := d.Exec(
		`INSERT INTO tbl_archive_runs(id, task_id, status, strategy, deleted_rows, rounds, message, sql_preview, started_at, finished_at, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.TaskID, r.Status, r.Strategy, r.DeletedRows, r.Rounds, r.Message, r.SQLPreview, r.StartedAt, r.FinishedAt, r.CreatedAt,
	)
	if err != nil {
		return err
	}
	return d.inheritTenantFromParent("tbl_archive_runs", r.ID, "tbl_archive_tasks", r.TaskID)
}

func (d *DB) UpdateArchiveRun(r *ArchiveRun) error {
	_, err := d.Exec(
		`UPDATE tbl_archive_runs SET status=?, deleted_rows=?, rounds=?, message=?, sql_preview=?, started_at=?, finished_at=? WHERE id=?`,
		r.Status, r.DeletedRows, r.Rounds, r.Message, r.SQLPreview, r.StartedAt, r.FinishedAt, r.ID,
	)
	return err
}

func (d *DB) GetArchiveRun(id string) (*ArchiveRun, error) {
	r := &ArchiveRun{}
	err := d.QueryRow(
		`SELECT id, task_id, status, strategy, deleted_rows, rounds, message, sql_preview, started_at, finished_at, created_at FROM tbl_archive_runs WHERE id=?`, id,
	).Scan(&r.ID, &r.TaskID, &r.Status, &r.Strategy, &r.DeletedRows, &r.Rounds, &r.Message, &r.SQLPreview, &r.StartedAt, &r.FinishedAt, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	decorateArchiveRun(r, nil)
	return r, nil
}

func (d *DB) ListArchiveRuns(taskID string, limit int) ([]ArchiveRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT r.id, r.task_id, r.status, r.strategy, r.deleted_rows, r.rounds, r.message, r.sql_preview, r.started_at, r.finished_at, r.created_at, COALESCE(t.name,'')
		FROM tbl_archive_runs r LEFT JOIN tbl_archive_tasks t ON t.id=r.task_id WHERE 1=1`
	var args []any
	if strings.TrimSpace(taskID) != "" {
		q += ` AND r.task_id=?`
		args = append(args, taskID)
	}
	q += ` ORDER BY r.created_at DESC LIMIT ` + strconv.Itoa(limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveRun
	for rows.Next() {
		var r ArchiveRun
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Status, &r.Strategy, &r.DeletedRows, &r.Rounds, &r.Message, &r.SQLPreview, &r.StartedAt, &r.FinishedAt, &r.CreatedAt, &r.TaskName); err != nil {
			return nil, err
		}
		decorateArchiveRun(&r, nil)
		out = append(out, r)
	}
	return out, rows.Err()
}

func decorateArchiveRun(r *ArchiveRun, t *ArchiveTask) {
	if r == nil {
		return
	}
	if t != nil && r.TaskName == "" {
		r.TaskName = t.Name
	}
	r.Volume = fmt.Sprintf("%d 行", r.DeletedRows)
	if r.StartedAt != "" && r.FinishedAt != "" {
		st, e1 := time.Parse(time.RFC3339, r.StartedAt)
		ed, e2 := time.Parse(time.RFC3339, r.FinishedAt)
		if e1 == nil && e2 == nil && ed.After(st) {
			r.Duration = ed.Sub(st).Truncate(time.Second).String()
		}
	}
}

func (d *DB) LastArchiveRunAt(taskID string) string {
	var at string
	_ = d.QueryRow(`SELECT COALESCE(started_at,'') FROM tbl_archive_runs WHERE task_id=? ORDER BY created_at DESC LIMIT 1`, taskID).Scan(&at)
	return at
}

func (d *DB) ArchiveApproved(t *ArchiveTask) bool {
	if t == nil {
		return false
	}
	if t.ApprovalStatus == StatusPending || t.ApprovalStatus == StatusRejected || t.ApprovalStatus == StatusCanceled {
		return false
	}
	if t.ApprovalStatus == "" && t.IssueID == "" {
		return true
	}
	return t.ApprovalStatus == StatusApproved
}

func (d *DB) UpdateCDCDescription(id, description string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_cdc_tasks SET description=?, updated_at=? WHERE id=?`, description, now, id)
	return err
}
