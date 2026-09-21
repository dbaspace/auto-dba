package store

import (
	"strings"
	"time"
)

type CDCLogFilter struct {
	TaskID               string
	MappingID            string
	LogLevel             string
	Step                 string
	Q                    string
	IncludeChildTaskLogs bool
	Limit                int
	Offset               int
}

func (d *DB) InsertCDCLog(it *CDCSyncLog) error {
	if d == nil || it == nil || strings.TrimSpace(it.TaskID) == "" {
		return nil
	}
	if strings.TrimSpace(it.Level) == "" {
		it.Level = "INFO"
	}
	if it.CreatedAt == "" {
		it.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := d.Exec(
		`INSERT INTO tbl_cdc_sync_logs(task_id, mapping_id, log_level, step, message, detail, operator, created_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		it.TaskID, strings.TrimSpace(it.MappingID), it.Level, it.Step, it.Message, it.Detail, it.Operator, it.CreatedAt,
	)
	return err
}

func (d *DB) ListCDCLogs(f CDCLogFilter) ([]CDCSyncLog, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 200 {
		f.Limit = 200
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	where, args := buildCDCLogWhere(f)
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_cdc_sync_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT id, task_id, COALESCE(mapping_id,''), log_level, step, message, COALESCE(detail,''), COALESCE(operator,''), created_at
FROM tbl_cdc_sync_logs` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, f.Limit, f.Offset)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []CDCSyncLog
	for rows.Next() {
		var it CDCSyncLog
		if err := rows.Scan(&it.ID, &it.TaskID, &it.MappingID, &it.Level, &it.Step, &it.Message, &it.Detail, &it.Operator, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	if out == nil {
		out = []CDCSyncLog{}
	}
	return out, total, rows.Err()
}

func buildCDCLogWhere(f CDCLogFilter) (string, []any) {
	var conds []string
	var args []any
	if s := strings.TrimSpace(f.TaskID); s != "" {
		if f.IncludeChildTaskLogs {
			conds = append(conds, `(task_id = ? OR task_id IN (SELECT id FROM tbl_cdc_tasks WHERE parent_id = ?))`)
			args = append(args, s, s)
		} else {
			conds = append(conds, `task_id = ?`)
			args = append(args, s)
		}
	}
	if s := strings.TrimSpace(f.MappingID); s != "" {
		conds = append(conds, `mapping_id = ?`)
		args = append(args, s)
	}
	if s := strings.TrimSpace(f.LogLevel); s != "" {
		conds = append(conds, `log_level = ?`)
		args = append(args, s)
	}
	if s := strings.TrimSpace(f.Step); s != "" {
		conds = append(conds, `step = ?`)
		args = append(args, s)
	}
	if s := strings.TrimSpace(f.Q); s != "" {
		like := "%" + s + "%"
		conds = append(conds, `(message ILIKE ? OR step ILIKE ? OR COALESCE(detail,'') ILIKE ?)`)
		args = append(args, like, like, like)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (d *DB) DeleteCDCLogsByTask(taskID string) error {
	if d == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	_, err := d.Exec(`DELETE FROM tbl_cdc_sync_logs WHERE task_id=?`, taskID)
	return err
}
