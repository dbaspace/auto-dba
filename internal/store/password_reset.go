package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const PasswordResetTTL = 30 * time.Minute

type PasswordResetToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    string
	CreatedAt time.Time
}

func hashResetToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// NewPasswordResetToken 生成明文 token 并落库（仅存 hash）。同用户旧 token 作废。
func (d *DB) NewPasswordResetToken(userID string) (plain string, err error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", errors.New("user_id required")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	plain = hex.EncodeToString(buf)
	hash := hashResetToken(plain)
	now := time.Now().UTC()
	expires := now.Add(PasswordResetTTL)
	if _, err := d.Exec(`UPDATE tbl_password_reset_tokens SET used_at=? WHERE user_id=? AND used_at=''`,
		now.Format(time.RFC3339), userID); err != nil {
		return "", err
	}
	_, err = d.Exec(
		`INSERT INTO tbl_password_reset_tokens (id, user_id, token_hash, expires_at, used_at, created_at)
		 VALUES (?,?,?,?,?,?)`,
		uuid.NewString(), userID, hash, expires.Format(time.RFC3339), "", now.Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	return plain, nil
}

func (d *DB) ConsumePasswordResetToken(plain string) (userID string, err error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", errors.New("token required")
	}
	hash := hashResetToken(plain)
	var id, uid, expiresAt, usedAt string
	err = d.QueryRow(
		`SELECT id, user_id, expires_at, COALESCE(used_at,'') FROM tbl_password_reset_tokens WHERE token_hash=?`,
		hash,
	).Scan(&id, &uid, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("重置链接无效或已过期")
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(usedAt) != "" {
		return "", errors.New("重置链接已使用")
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().UTC().After(exp) {
		return "", errors.New("重置链接无效或已过期")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := d.Exec(`UPDATE tbl_password_reset_tokens SET used_at=? WHERE id=? AND used_at=''`, now, id)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", errors.New("重置链接已使用")
	}
	return uid, nil
}

// CountRecentPasswordResets 统计 window 内该用户申请次数（含已用）。
func (d *DB) CountRecentPasswordResets(userID string, window time.Duration) (int, error) {
	since := time.Now().UTC().Add(-window).Format(time.RFC3339)
	var n int
	err := d.QueryRow(
		`SELECT COUNT(1) FROM tbl_password_reset_tokens WHERE user_id=? AND created_at>=?`,
		userID, since,
	).Scan(&n)
	return n, err
}

func (d *DB) GetUserByEmail(email string) (*User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, nil
	}
	u := &User{}
	var roleID sql.NullString
	var source sql.NullString
	var disabled, failed int
	var lockedAt string
	err := d.QueryRow(
		`SELECT id, username, display_name, role, role_id, password_hash, source, created_at,
			COALESCE(login_disabled,0), COALESCE(failed_login_count,0), COALESCE(locked_at,'')
		 FROM tbl_users
		 WHERE lower(COALESCE(profile::json->>'email','')) = ?
		 LIMIT 1`,
		email,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &roleID, &u.PasswordHash, &source, &u.CreatedAt,
		&disabled, &failed, &lockedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.RoleID = roleID.String
	u.Source = userSource(source.String)
	scanUserLoginCols(disabled, failed, lockedAt, u)
	d.attachUserProfile(u)
	d.AttachUserRoleIDs(u)
	return u, nil
}

func (d *DB) ensurePasswordResetTokensTable() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS tbl_password_reset_tokens (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		token_hash TEXT NOT NULL UNIQUE,
		expires_at TEXT NOT NULL,
		used_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE INDEX IF NOT EXISTS idx_pwd_reset_user ON tbl_password_reset_tokens(user_id)`)
	return err
}
