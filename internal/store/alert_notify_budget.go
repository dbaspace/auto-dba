package store

import (
	"database/sql"
	"strings"
	"time"
)

// AlertNotifyBudget 按 fingerprint 的 trigger/repeat 窗口预算（跨 resolve 重建告警仍累计）。
type AlertNotifyBudget struct {
	Fingerprint    string
	NotifyCount    int
	LastNotifiedAt string
	UpdatedAt      string
}

func (d *DB) ensureAlertNotifyBudgetTable() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_alert_notify_budget (
		fingerprint TEXT PRIMARY KEY,
		notify_count INTEGER NOT NULL DEFAULT 0,
		last_notified_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	return err
}

// GetAlertNotifyBudget 读取指纹预算；无记录返回零值。
func (d *DB) GetAlertNotifyBudget(fingerprint string) (*AlertNotifyBudget, error) {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return &AlertNotifyBudget{}, nil
	}
	var b AlertNotifyBudget
	err := d.QueryRow(
		`SELECT fingerprint, notify_count, last_notified_at, updated_at FROM tbl_alert_notify_budget WHERE fingerprint=?`,
		fingerprint,
	).Scan(&b.Fingerprint, &b.NotifyCount, &b.LastNotifiedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return &AlertNotifyBudget{Fingerprint: fingerprint}, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// UpsertAlertNotifyBudget 写入指纹预算。
func (d *DB) UpsertAlertNotifyBudget(b *AlertNotifyBudget) error {
	if b == nil || strings.TrimSpace(b.Fingerprint) == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_alert_notify_budget(fingerprint, notify_count, last_notified_at, updated_at)
		VALUES(?,?,?,?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			notify_count=EXCLUDED.notify_count,
			last_notified_at=EXCLUDED.last_notified_at,
			updated_at=EXCLUDED.updated_at`,
		b.Fingerprint, b.NotifyCount, b.LastNotifiedAt, b.UpdatedAt)
	return err
}

// ApplyFingerprintNotifyMark 按策略累加指纹预算，并同步到告警行计数。
func (d *DB) ApplyFingerprintNotifyMark(a *Alert, policy AlertNotifyPolicy, now time.Time) error {
	if a == nil {
		return nil
	}
	policy = ResolveAlertNotifyPolicy(policy)
	b, err := d.GetAlertNotifyBudget(a.Fingerprint)
	if err != nil {
		return err
	}
	if NotifyWindowElapsed(b.LastNotifiedAt, policy.Window, now) {
		b.NotifyCount = 1
	} else {
		b.NotifyCount++
	}
	b.LastNotifiedAt = now.UTC().Format(time.RFC3339)
	b.Fingerprint = a.Fingerprint
	if err := d.UpsertAlertNotifyBudget(b); err != nil {
		return err
	}
	a.NotifyCount = b.NotifyCount
	a.LastNotifiedAt = b.LastNotifiedAt
	return nil
}

// LoadFingerprintNotifyIntoAlert 将指纹预算填入告警行供 Decide 使用。
func (d *DB) LoadFingerprintNotifyIntoAlert(a *Alert) error {
	if a == nil || strings.TrimSpace(a.Fingerprint) == "" {
		return nil
	}
	b, err := d.GetAlertNotifyBudget(a.Fingerprint)
	if err != nil {
		return err
	}
	if b.LastNotifiedAt != "" || b.NotifyCount > 0 {
		a.NotifyCount = b.NotifyCount
		a.LastNotifiedAt = b.LastNotifiedAt
	}
	return nil
}
