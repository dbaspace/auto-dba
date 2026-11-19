package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ArchiveAlertRule struct {
	ID        string  `json:"id"`
	RuleType  string  `json:"rule_type"`
	Title     string  `json:"title"`
	Threshold float64 `json:"threshold"`
	Severity  string  `json:"severity"`
	Enabled   bool    `json:"enabled"`
	Status    string  `json:"status,omitempty"`
	CreatedAt string  `json:"created_at,omitempty"`
	UpdatedAt string  `json:"updated_at,omitempty"`
}

type ArchiveAlertEvent struct {
	ID          string `json:"id"`
	RuleID      string `json:"rule_id"`
	RuleType    string `json:"rule_type"`
	Fingerprint string `json:"fingerprint"`
	Message     string `json:"message"`
	Severity    string `json:"severity"`
	Acked       bool   `json:"acked"`
	CreatedAt   string `json:"created_at"`
}

func (d *DB) ListArchiveAlertRules() ([]ArchiveAlertRule, error) {
	rows, err := d.Query(`SELECT id, rule_type, title, threshold, severity, enabled, created_at, updated_at
		FROM tbl_archive_alert_rules ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveAlertRule
	for rows.Next() {
		var r ArchiveAlertRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.RuleType, &r.Title, &r.Threshold, &r.Severity, &enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.Status = "normal"
		out = append(out, r)
	}
	if out == nil {
		out = []ArchiveAlertRule{}
	}
	return out, rows.Err()
}

func (d *DB) CountArchiveAlertRules() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_archive_alert_rules`).Scan(&n)
	return n, err
}

func (d *DB) GetArchiveAlertRuleByType(ruleType string) (*ArchiveAlertRule, error) {
	var r ArchiveAlertRule
	var enabled int
	err := d.QueryRow(`SELECT id, rule_type, title, threshold, severity, enabled, created_at, updated_at
		FROM tbl_archive_alert_rules WHERE rule_type=?`, ruleType).
		Scan(&r.ID, &r.RuleType, &r.Title, &r.Threshold, &r.Severity, &enabled, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	return &r, nil
}

func (d *DB) InsertArchiveAlertRule(r *ArchiveAlertRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	_, err := d.Exec(`INSERT INTO tbl_archive_alert_rules(id, rule_type, title, threshold, severity, enabled, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.RuleType, r.Title, r.Threshold, r.Severity, enabled, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) UpdateArchiveAlertRule(r *ArchiveAlertRule) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	_, err := d.Exec(`UPDATE tbl_archive_alert_rules SET title=?, threshold=?, severity=?, enabled=?, updated_at=? WHERE id=?`,
		r.Title, r.Threshold, r.Severity, enabled, now, r.ID)
	return err
}

func (d *DB) DeleteArchiveAlertRule(id string) error {
	if _, err := d.Exec(`DELETE FROM tbl_archive_alert_events WHERE rule_id=?`, id); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM tbl_archive_alert_rules WHERE id=?`, id)
	return err
}

func (d *DB) ListArchiveAlertEvents(limit int) ([]ArchiveAlertEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := d.Query(`SELECT id, rule_id, rule_type, fingerprint, message, severity, acked, created_at
		FROM tbl_archive_alert_events ORDER BY created_at DESC LIMIT ` + itoaLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveAlertEvent
	for rows.Next() {
		var e ArchiveAlertEvent
		var acked int
		if err := rows.Scan(&e.ID, &e.RuleID, &e.RuleType, &e.Fingerprint, &e.Message, &e.Severity, &acked, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Acked = acked != 0
		out = append(out, e)
	}
	if out == nil {
		out = []ArchiveAlertEvent{}
	}
	return out, rows.Err()
}

func itoaLimit(n int) string {
	if n < 1 {
		n = 1
	}
	if n > 200 {
		n = 200
	}
	return strconv.Itoa(n)
}

func (d *DB) InsertArchiveAlertEvent(e *ArchiveAlertEvent) (bool, error) {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt == "" {
		e.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	res, err := d.Exec(`INSERT INTO tbl_archive_alert_events(id, rule_id, rule_type, fingerprint, message, severity, acked, created_at)
		VALUES(?,?,?,?,?,?,0,?) ON CONFLICT (fingerprint) DO NOTHING`,
		e.ID, e.RuleID, e.RuleType, e.Fingerprint, e.Message, e.Severity, e.CreatedAt)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (d *DB) AckArchiveAlertEvent(id string) error {
	_, err := d.Exec(`UPDATE tbl_archive_alert_events SET acked=1 WHERE id=?`, id)
	return err
}

func (d *DB) CountArchiveRunsSince(status, since string) (int, error) {
	q := `SELECT COUNT(1) FROM tbl_archive_runs WHERE status=?`
	args := []any{status}
	if strings.TrimSpace(since) != "" {
		q += ` AND COALESCE(NULLIF(started_at,''), created_at) >= ?`
		args = append(args, since)
	}
	var n int
	err := d.QueryRow(q, args...).Scan(&n)
	return n, err
}

func (d *DB) ListArchiveRunsSince(status, since string, limit int) ([]ArchiveRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT ` + archiveRunCols + ` FROM tbl_archive_runs r LEFT JOIN tbl_archive_tasks t ON t.id=r.task_id WHERE r.status=?`
	args := []any{status}
	if strings.TrimSpace(since) != "" {
		q += ` AND COALESCE(NULLIF(r.started_at,''), r.created_at) >= ?`
		args = append(args, since)
	}
	q += ` ORDER BY r.created_at DESC LIMIT ` + itoaLimit(limit)
	return d.queryArchiveRuns(q, args...)
}

func (d *DB) TryLockArchiveInstance(instanceID, taskID string) error {
	instanceID = strings.TrimSpace(instanceID)
	taskID = strings.TrimSpace(taskID)
	if instanceID == "" || taskID == "" {
		return nil
	}
	stale := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
	_, _ = d.Exec(`DELETE FROM tbl_archive_instance_locks l
		WHERE l.acquired_at < ?
		  AND NOT EXISTS (
			SELECT 1 FROM tbl_archive_tasks t WHERE t.id = l.task_id AND t.status = ?
		)`, stale, StatusRunning)
	var owner string
	err := d.QueryRow(`SELECT task_id FROM tbl_archive_instance_locks WHERE instance_id=?`, instanceID).Scan(&owner)
	if err == nil {
		if owner == taskID {
			return nil
		}
		return errors.New("同一实例已有归档在执行")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`INSERT INTO tbl_archive_instance_locks(instance_id, task_id, acquired_at) VALUES(?,?,?)`,
		instanceID, taskID, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return errors.New("同一实例已有归档在执行")
		}
		return err
	}
	return nil
}

func (d *DB) UnlockArchiveInstance(instanceID, taskID string) {
	instanceID = strings.TrimSpace(instanceID)
	taskID = strings.TrimSpace(taskID)
	if instanceID == "" {
		return
	}
	_, _ = d.Exec(`DELETE FROM tbl_archive_instance_locks WHERE instance_id=? AND task_id=?`, instanceID, taskID)
}
