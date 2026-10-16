package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// UserSession 元库最终态镜像（非展示权威源）。
type UserSession struct {
	ID        string
	UserID    string
	Username  string
	TenantID  string
	JTI       string
	Status    string // online | offline | expired
	IP        string
	UserAgent string
	Kind      string
	OnlineAt  string
	ExpireAt  string
	OfflineAt string
	Reason    string
	KickedBy  string
	CreatedAt string
	UpdatedAt string
}

func (d *DB) ensureUserSessionsTable() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_user_sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		username TEXT NOT NULL DEFAULT '',
		tenant_id TEXT NOT NULL DEFAULT '',
		jti TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'offline',
		ip TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		kind TEXT NOT NULL DEFAULT 'normal',
		online_at TEXT NOT NULL DEFAULT '',
		expire_at TEXT NOT NULL DEFAULT '',
		offline_at TEXT NOT NULL DEFAULT '',
		reason TEXT NOT NULL DEFAULT '',
		kicked_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	if _, err = d.Exec(`CREATE INDEX IF NOT EXISTS idx_user_sessions_user ON tbl_user_sessions(user_id)`); err != nil {
		return err
	}
	if _, err = d.Exec(`CREATE INDEX IF NOT EXISTS idx_user_sessions_status ON tbl_user_sessions(status)`); err != nil {
		return err
	}
	if _, err = d.Exec(`CREATE INDEX IF NOT EXISTS idx_user_sessions_jti ON tbl_user_sessions(jti)`); err != nil {
		return err
	}
	return nil
}

// UpsertUserSessionOnline 登录镜像（Redis 成功后调用）。
func (d *DB) UpsertUserSessionOnline(it *UserSession) error {
	if it == nil || it.UserID == "" || it.JTI == "" {
		return fmt.Errorf("user session incomplete")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if it.Kind == "" {
		it.Kind = "normal"
	}
	it.Status = "online"
	it.UpdatedAt = now
	if it.OnlineAt == "" {
		it.OnlineAt = now
	}
	var existing string
	err := d.QueryRow(`SELECT id FROM tbl_user_sessions WHERE jti=?`, it.JTI).Scan(&existing)
	if err == nil && existing != "" {
		_, err = d.Exec(`
			UPDATE tbl_user_sessions SET
				user_id=?, username=?, tenant_id=?, status='online', ip=?, user_agent=?, kind=?,
				online_at=?, expire_at=?, offline_at='', reason='', kicked_by='', updated_at=?
			WHERE id=?
		`, it.UserID, it.Username, it.TenantID, it.IP, it.UserAgent, it.Kind,
			it.OnlineAt, it.ExpireAt, it.UpdatedAt, existing)
		return err
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	if it.CreatedAt == "" {
		it.CreatedAt = now
	}
	_, err = d.Exec(`
		INSERT INTO tbl_user_sessions (
			id, user_id, username, tenant_id, jti, status, ip, user_agent, kind,
			online_at, expire_at, offline_at, reason, kicked_by, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	`, it.ID, it.UserID, it.Username, it.TenantID, it.JTI, it.Status, it.IP, it.UserAgent, it.Kind,
		it.OnlineAt, it.ExpireAt, "", "", "", it.CreatedAt, it.UpdatedAt)
	return err
}

// MarkUserSessionOffline 按 jti 下线。
func (d *DB) MarkUserSessionOffline(jti, reason, kickedBy string) error {
	jti = strings.TrimSpace(jti)
	if jti == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`
		UPDATE tbl_user_sessions SET status=?, offline_at=?, reason=?, kicked_by=?, updated_at=?
		WHERE jti=? AND status='online'
	`, "offline", now, reason, kickedBy, now, jti)
	return err
}

// MarkUserSessionsOfflineAll 用户全端下线。
func (d *DB) MarkUserSessionsOfflineAll(userID, reason, kickedBy string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`
		UPDATE tbl_user_sessions SET status=?, offline_at=?, reason=?, kicked_by=?, updated_at=?
		WHERE user_id=? AND status='online'
	`, "offline", now, reason, kickedBy, now, userID)
	return err
}

// ListOnlineUserSessionRows 对账：元库仍标 online 的行。
func (d *DB) ListOnlineUserSessionRows(limit int) ([]UserSession, error) {
	if limit <= 0 {
		limit = 5000
	}
	rows, err := d.Query(`
		SELECT id, user_id, username, tenant_id, jti, status, ip, user_agent, kind,
			online_at, expire_at, offline_at, reason, kicked_by, created_at, updated_at
		FROM tbl_user_sessions WHERE status='online' ORDER BY updated_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSession
	for rows.Next() {
		var it UserSession
		if err := rows.Scan(&it.ID, &it.UserID, &it.Username, &it.TenantID, &it.JTI, &it.Status,
			&it.IP, &it.UserAgent, &it.Kind, &it.OnlineAt, &it.ExpireAt, &it.OfflineAt,
			&it.Reason, &it.KickedBy, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// MarkUserSessionExpired 对账标过期。
func (d *DB) MarkUserSessionExpired(jti string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`
		UPDATE tbl_user_sessions SET status='expired', offline_at=?, reason='ttl_expired', updated_at=?
		WHERE jti=? AND status='online'
	`, now, now, jti)
	return err
}
