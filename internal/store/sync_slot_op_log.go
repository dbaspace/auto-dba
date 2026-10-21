package store

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	SyncSlotOpCollect = "collect"
	SyncSlotOpRestart = "restart"
	syncSlotOpLogKeep = 3000
)

// SyncSlotOpLog 同步列表采集/重启操作日志（持久化）。
type SyncSlotOpLog struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id,omitempty"`
	Kind      string `json:"kind"`
	Line      string `json:"line"`
	CreatedAt string `json:"created_at"`
}

func (d *DB) ensureSyncSlotOpLogsTable() error {
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_sync_slot_op_logs (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT '',
		line TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_slot_op_logs_tenant_created
		ON tbl_sync_slot_op_logs(tenant_id, created_at DESC)`); err != nil {
		return err
	}
	_, err := d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_slot_op_logs_kind_created
		ON tbl_sync_slot_op_logs(kind, created_at DESC)`)
	return err
}

// AppendSyncSlotOpLog 追加一条操作日志，并裁剪租户内超量旧记录。
func (d *DB) AppendSyncSlotOpLog(tenantID, kind, line string) error {
	if d == nil {
		return nil
	}
	_ = d.ensureSyncSlotOpLogsTable()
	tenantID = strings.TrimSpace(tenantID)
	kind = strings.TrimSpace(kind)
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	if kind == "" {
		kind = SyncSlotOpRestart
	}
	id := uuid.NewString()
	now := time.Now().Format(time.RFC3339)
	if _, err := d.Exec(
		`INSERT INTO tbl_sync_slot_op_logs(id, tenant_id, kind, line, created_at) VALUES(?,?,?,?,?)`,
		id, tenantID, kind, line, now,
	); err != nil {
		return err
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_sync_slot_op_logs WHERE tenant_id=?`, tenantID).Scan(&n); err != nil {
		return nil
	}
	if n <= syncSlotOpLogKeep {
		return nil
	}
	drop := n - syncSlotOpLogKeep
	_, _ = d.Exec(`
		DELETE FROM tbl_sync_slot_op_logs WHERE id IN (
			SELECT id FROM tbl_sync_slot_op_logs WHERE tenant_id=? ORDER BY created_at ASC LIMIT ?
		)`, tenantID, drop)
	return nil
}

// ListSyncSlotOpLogs 按时间倒序分页返回操作日志（第 1 页为最新）；kind 空则全部。
func (d *DB) ListSyncSlotOpLogs(tenantID, kind string, limit, offset int) ([]SyncSlotOpLog, int, error) {
	if d == nil {
		return nil, 0, nil
	}
	_ = d.ensureSyncSlotOpLogsTable()
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	tenantID = strings.TrimSpace(tenantID)
	kind = strings.TrimSpace(kind)
	where := `WHERE tenant_id=?`
	args := []any{tenantID}
	if kind != "" {
		where += ` AND kind=?`
		args = append(args, kind)
	}
	var total int
	if err := d.QueryRow(`SELECT COUNT(1) FROM tbl_sync_slot_op_logs `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT id, tenant_id, kind, line, created_at FROM tbl_sync_slot_op_logs ` + where + `
		ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]any{}, args...), limit, offset)
	rows, err := d.Query(q, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SyncSlotOpLog
	for rows.Next() {
		var it SyncSlotOpLog
		if err := rows.Scan(&it.ID, &it.TenantID, &it.Kind, &it.Line, &it.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}
