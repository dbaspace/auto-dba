package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const fbTaskCols = `id, name, status, instance_id, db_name, start_time, end_time, sql_preview,
	host, port, tables, sql_type, output_kind, reviewer, error_message, warning, work_dir,
	wal_bytes, wal_files, change_count, log_total, log_done, parse_total, parse_done,
	dml_ticket_id, created_by, start_xid, stop_xid, start_file, start_pos, stop_file, stop_pos,
	started_at, finished_at, created_at, updated_at, issue_id, approval_status`

func attachFlashbackJSON(t *FlashbackTask) {
	t.Database = t.DBName
	t.TargetTime = t.StartTime
	if t.Tables != "" {
		_ = json.Unmarshal([]byte(t.Tables), &t.TableList)
	}
	if t.TableList == nil {
		t.TableList = []string{}
	}
}

func scanFlashback(s interface{ Scan(...any) error }, t *FlashbackTask) error {
	err := s.Scan(
		&t.ID, &t.Name, &t.Status, &t.InstanceID, &t.DBName, &t.StartTime, &t.EndTime, &t.SQLPreview,
		&t.Host, &t.Port, &t.Tables, &t.SQLType, &t.OutputKind, &t.Reviewer, &t.ErrorMessage, &t.Warning, &t.WorkDir,
		&t.WALBytes, &t.WALFiles, &t.ChangeCount, &t.LogTotal, &t.LogDone, &t.ParseTotal, &t.ParseDone,
		&t.DMLTicketID, &t.CreatedBy, &t.StartXID, &t.StopXID, &t.StartFile, &t.StartPos, &t.StopFile, &t.StopPos,
		&t.StartedAt, &t.FinishedAt, &t.CreatedAt, &t.UpdatedAt, &t.IssueID, &t.ApprovalStatus,
	)
	if err != nil {
		return err
	}
	attachFlashbackJSON(t)
	return nil
}

func (d *DB) CreateFlashback(t *FlashbackTask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = StatusPending
	}
	if t.DBName == "" {
		t.DBName = t.Database
	}
	if t.StartTime == "" {
		t.StartTime = t.TargetTime
	}
	if t.OutputKind == "" {
		t.OutputKind = "flashback"
	}
	if t.Tables == "" {
		if len(t.TableList) > 0 {
			b, _ := json.Marshal(t.TableList)
			t.Tables = string(b)
		} else {
			t.Tables = "[]"
		}
	}
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := d.Exec(
		`INSERT INTO tbl_flashback_tasks(`+fbTaskCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Name, t.Status, t.InstanceID, t.DBName, t.StartTime, t.EndTime, t.SQLPreview,
		t.Host, t.Port, t.Tables, t.SQLType, t.OutputKind, t.Reviewer, t.ErrorMessage, t.Warning, t.WorkDir,
		t.WALBytes, t.WALFiles, t.ChangeCount, t.LogTotal, t.LogDone, t.ParseTotal, t.ParseDone,
		t.DMLTicketID, t.CreatedBy, t.StartXID, t.StopXID, t.StartFile, t.StartPos, t.StopFile, t.StopPos,
		t.StartedAt, t.FinishedAt, t.CreatedAt, t.UpdatedAt, t.IssueID, t.ApprovalStatus,
	)
	return err
}

func (d *DB) ListFlashbacks() ([]FlashbackTask, error) {
	rows, err := d.Query(`SELECT ` + fbTaskCols + ` FROM tbl_flashback_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FlashbackTask
	for rows.Next() {
		var t FlashbackTask
		if err := scanFlashback(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) GetFlashback(id string) (*FlashbackTask, error) {
	t := &FlashbackTask{}
	err := scanFlashback(d.QueryRow(`SELECT `+fbTaskCols+` FROM tbl_flashback_tasks WHERE id=?`, id), t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (d *DB) GetFlashbackByIssueID(issueID string) (*FlashbackTask, error) {
	if strings.TrimSpace(issueID) == "" {
		return nil, nil
	}
	t := &FlashbackTask{}
	err := scanFlashback(d.QueryRow(`SELECT `+fbTaskCols+` FROM tbl_flashback_tasks WHERE issue_id=? ORDER BY created_at DESC LIMIT 1`, issueID), t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (d *DB) UpdateFlashbackApproval(id, status, approval string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_flashback_tasks SET status=?, approval_status=?, updated_at=? WHERE id=?`, status, approval, now, id)
	return err
}

func (d *DB) UpdateFlashback(t *FlashbackTask) error {
	now := time.Now().UTC().Format(time.RFC3339)
	t.UpdatedAt = now
	_, err := d.Exec(
		`UPDATE tbl_flashback_tasks SET name=?, status=?, instance_id=?, db_name=?, start_time=?, end_time=?, sql_preview=?,
			host=?, port=?, tables=?, sql_type=?, output_kind=?, reviewer=?, error_message=?, warning=?, work_dir=?,
			wal_bytes=?, wal_files=?, change_count=?, log_total=?, log_done=?, parse_total=?, parse_done=?,
			dml_ticket_id=?, created_by=?, start_xid=?, stop_xid=?, start_file=?, start_pos=?, stop_file=?, stop_pos=?,
			started_at=?, finished_at=?, updated_at=?, issue_id=?, approval_status=? WHERE id=?`,
		t.Name, t.Status, t.InstanceID, t.DBName, t.StartTime, t.EndTime, t.SQLPreview,
		t.Host, t.Port, t.Tables, t.SQLType, t.OutputKind, t.Reviewer, t.ErrorMessage, t.Warning, t.WorkDir,
		t.WALBytes, t.WALFiles, t.ChangeCount, t.LogTotal, t.LogDone, t.ParseTotal, t.ParseDone,
		t.DMLTicketID, t.CreatedBy, t.StartXID, t.StopXID, t.StartFile, t.StartPos, t.StopFile, t.StopPos,
		t.StartedAt, t.FinishedAt, t.UpdatedAt, t.IssueID, t.ApprovalStatus, t.ID,
	)
	return err
}

func (d *DB) UpdateFlashbackStatus(id, status, errMsg, warning string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	started, finished := "", ""
	if status == StatusRunning {
		started = now
	}
	if status == StatusSucceeded || status == StatusFailed || status == StatusCanceled || status == "cancelled" {
		finished = now
	}
	_, err := d.Exec(
		`UPDATE tbl_flashback_tasks SET status=?, error_message=?, warning=?, updated_at=?,
			started_at=CASE WHEN ?<>'' AND (started_at='' OR started_at IS NULL) THEN ? ELSE started_at END,
			finished_at=CASE WHEN ?<>'' THEN ? ELSE finished_at END
		 WHERE id=?`,
		status, errMsg, warning, now, started, started, finished, finished, id,
	)
	return err
}

func (d *DB) UpdateFlashbackProgress(id, workDir string, walBytes int64, walFiles, changeCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_flashback_tasks SET work_dir=?, wal_bytes=?, wal_files=?, change_count=?, updated_at=? WHERE id=?`,
		workDir, walBytes, walFiles, changeCount, now, id)
	return err
}

func (d *DB) UpdateFlashbackStage(id string, logDone, logTotal, parseDone, parseTotal int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_flashback_tasks SET log_done=?, log_total=?, parse_done=?, parse_total=?, updated_at=? WHERE id=?`,
		logDone, logTotal, parseDone, parseTotal, now, id)
	return err
}

func (d *DB) SetFlashbackDMLTicket(id, ticketID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_flashback_tasks SET dml_ticket_id=?, updated_at=? WHERE id=?`, ticketID, now, id)
	return err
}

func (d *DB) FailStuckFlashbacks(msg string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := d.Exec(`UPDATE tbl_flashback_tasks SET status=?, error_message=?, finished_at=?, updated_at=? WHERE status=?`,
		StatusFailed, msg, now, now, StatusRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *DB) AddFlashbackLog(taskID, level, message string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO tbl_flashback_logs(task_id, level, message, created_at) VALUES(?,?,?,?)`, taskID, level, message, now)
	return err
}

func (d *DB) ListFlashbackLogs(taskID string, limit int) ([]FlashbackLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := d.Query(`SELECT id, task_id, level, message, created_at FROM tbl_flashback_logs WHERE task_id=? ORDER BY id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FlashbackLog
	for rows.Next() {
		var l FlashbackLog
		if err := rows.Scan(&l.ID, &l.TaskID, &l.Level, &l.Message, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (d *DB) InsertFlashbackSQLs(rows []FlashbackSQL) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range rows {
		r := &rows[i]
		if r.TS == "" {
			r.TS = now
		}
		if _, err := d.Exec(
			`INSERT INTO tbl_flashback_sqls(task_id, seq, kind, schema_name, table_name, op, xid, ts, statement, risk) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			r.TaskID, r.Seq, r.Kind, r.SchemaName, r.TableName, r.Op, r.XID, r.TS, r.Statement, r.Risk,
		); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ListFlashbackSQLs(taskID, kind string, ops []string, limit, offset int) ([]FlashbackSQL, int, error) {
	q := `SELECT id, task_id, seq, kind, schema_name, table_name, op, xid, ts, statement, risk FROM tbl_flashback_sqls WHERE task_id=?`
	args := []any{taskID}
	if strings.TrimSpace(kind) != "" {
		q += ` AND kind=?`
		args = append(args, strings.TrimSpace(kind))
	}
	if len(ops) > 0 {
		ph := make([]string, 0, len(ops))
		for _, op := range ops {
			op = strings.ToUpper(strings.TrimSpace(op))
			if op == "" {
				continue
			}
			ph = append(ph, "?")
			args = append(args, op)
		}
		if len(ph) > 0 {
			q += ` AND op IN (` + strings.Join(ph, ",") + `)`
		}
	}
	var total int
	countQ := `SELECT COUNT(1) FROM (` + q + `) t`
	if err := d.QueryRow(countQ, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 200
	}
	q += ` ORDER BY seq LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []FlashbackSQL
	for rows.Next() {
		var r FlashbackSQL
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Seq, &r.Kind, &r.SchemaName, &r.TableName, &r.Op, &r.XID, &r.TS, &r.Statement, &r.Risk); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (d *DB) ListFlashbackSQLStatements(taskID, kind string, ops []string) ([]string, error) {
	rows, _, err := d.ListFlashbackSQLs(taskID, kind, ops, 5000, 0)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if strings.TrimSpace(r.Statement) != "" {
			out = append(out, r.Statement)
		}
	}
	return out, nil
}
