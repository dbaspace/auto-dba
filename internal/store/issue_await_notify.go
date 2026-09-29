package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const adbaNotifyKey = "_adba_notify"
const awaitStuckAtKey = "await_exec_stuck_at"

// ListStaleApprovedIssues 返回仍为已通过且 updated_at 早于 before 的工单。
func (d *DB) ListStaleApprovedIssues(before time.Time) ([]Issue, error) {
	cutoff := before.UTC().Format(time.RFC3339)
	rows, err := d.Query(
		`SELECT id, ticket_no, issue_type, instance_id, database_id, db_name, sql_content, description, status, submitter, assigned_to, reject_reason, need_rollback, scheduled_at, execute_result, created_at, updated_at
		 FROM tbl_issues WHERE status=? AND updated_at<? ORDER BY updated_at ASC`,
		StatusApproved, cutoff,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var it Issue
		var exec sql.NullString
		if err := rows.Scan(
			&it.ID, &it.TicketNo, &it.IssueType, &it.InstanceID, &it.DatabaseID, &it.DBName, &it.SQLContent, &it.Description,
			&it.Status, &it.Submitter, &it.AssignedTo, &it.RejectReason, &it.NeedRollback, &it.ScheduledAt, &exec,
			&it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if exec.Valid {
			it.ExecuteResult = exec.String
		}
		it.Type = it.IssueType
		out = append(out, it)
	}
	return out, rows.Err()
}

// IssueAwaitStuckAlreadyNotified 是否已发过待执行超时告警。
func IssueAwaitStuckAlreadyNotified(it *Issue) bool {
	if it == nil {
		return false
	}
	if v := adbaNotifyValue(it.NeedRollback, awaitStuckAtKey); v != "" {
		return true
	}
	if v := execNotifyAwaitStuck(it.ExecuteResult); v != "" {
		return true
	}
	return false
}

// MarkIssueAwaitStuckNotified 落库防重标记（优先 merge need_rollback；否则写 execute_result 旁路）。
func (d *DB) MarkIssueAwaitStuckNotified(id string) error {
	it, err := d.GetIssue(id)
	if err != nil {
		return err
	}
	if it == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if merged, ok := mergeAdbaNotify(it.NeedRollback, awaitStuckAtKey, now); ok {
		_, err = d.Exec(`UPDATE tbl_issues SET need_rollback=?, updated_at=? WHERE id=?`, merged, now, id)
		return err
	}
	mergedExec, ok := mergeExecNotifyAwaitStuck(it.ExecuteResult, now)
	if !ok {
		return nil
	}
	_, err = d.Exec(`UPDATE tbl_issues SET execute_result=?, updated_at=? WHERE id=?`, mergedExec, now, id)
	return err
}

func adbaNotifyValue(needRollback, key string) string {
	raw := strings.TrimSpace(needRollback)
	if raw == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	n, _ := m[adbaNotifyKey].(map[string]any)
	if n == nil {
		return ""
	}
	v, _ := n[key].(string)
	return strings.TrimSpace(v)
}

func mergeAdbaNotify(needRollback, key, value string) (string, bool) {
	raw := strings.TrimSpace(needRollback)
	var m map[string]any
	if raw == "" {
		m = map[string]any{}
	} else if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", false
	}
	n, _ := m[adbaNotifyKey].(map[string]any)
	if n == nil {
		n = map[string]any{}
	}
	n[key] = value
	m[adbaNotifyKey] = n
	b, err := json.Marshal(m)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func execNotifyAwaitStuck(execResult string) string {
	raw := strings.TrimSpace(execResult)
	if raw == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	n, _ := m["_notify"].(map[string]any)
	if n == nil {
		return ""
	}
	v, _ := n["await_stuck"].(string)
	return strings.TrimSpace(v)
}

func mergeExecNotifyAwaitStuck(execResult, value string) (string, bool) {
	raw := strings.TrimSpace(execResult)
	var m map[string]any
	if raw == "" {
		m = map[string]any{}
	} else if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", false
	}
	n, _ := m["_notify"].(map[string]any)
	if n == nil {
		n = map[string]any{}
	}
	n["await_stuck"] = value
	m["_notify"] = n
	b, err := json.Marshal(m)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// ScheduledAtDueOrEmpty scheduled_at 为空或已到期则视为可计超时。
func ScheduledAtDueOrEmpty(scheduledAt string, now time.Time) bool {
	raw := strings.TrimSpace(scheduledAt)
	if raw == "" {
		return true
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return !t.After(now)
		}
		if t, err := time.Parse(layout, raw); err == nil {
			return !t.After(now)
		}
	}
	// 无法解析则按已到期处理，避免永久跳过告警
	return true
}
