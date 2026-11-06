package store

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ExportWhitelist 导出白名单。ExpireAt 为空表示不过期。
type ExportWhitelist struct {
	ID        string   `json:"id"`
	Username  string   `json:"username"`
	DBIDs     []string `json:"db_ids"`
	ExpireAt  string   `json:"expire_at"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

func (d *DB) ensureExportWhitelistTable() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_export_whitelist (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL DEFAULT '',
		db_ids TEXT NOT NULL DEFAULT '[]',
		expire_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = d.Exec(`CREATE INDEX IF NOT EXISTS idx_export_whitelist_user ON tbl_export_whitelist(username)`)
	return err
}

func (d *DB) ListExportWhitelist() ([]ExportWhitelist, error) {
	rows, err := d.Query(`SELECT id, username, db_ids, expire_at, created_at, updated_at
		FROM tbl_export_whitelist ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportWhitelist{}
	for rows.Next() {
		var it ExportWhitelist
		var raw string
		if err := rows.Scan(&it.ID, &it.Username, &raw, &it.ExpireAt, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		it.DBIDs = parseExportDBIDs(raw)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) CreateExportWhitelist(it *ExportWhitelist) error {
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	it.CreatedAt = now
	it.UpdatedAt = now
	it.Username = strings.TrimSpace(it.Username)
	raw, err := json.Marshal(cleanDBIDs(it.DBIDs))
	if err != nil {
		return err
	}
	_, err = d.Exec(`INSERT INTO tbl_export_whitelist(id, username, db_ids, expire_at, created_at, updated_at)
		VALUES(?,?,?,?,?,?)`, it.ID, it.Username, string(raw), strings.TrimSpace(it.ExpireAt), it.CreatedAt, it.UpdatedAt)
	return err
}

func (d *DB) UpdateExportWhitelist(it *ExportWhitelist) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	it.Username = strings.TrimSpace(it.Username)
	raw, err := json.Marshal(cleanDBIDs(it.DBIDs))
	if err != nil {
		return err
	}
	res, err := d.Exec(`UPDATE tbl_export_whitelist SET username=?, db_ids=?, expire_at=?, updated_at=? WHERE id=?`,
		it.Username, string(raw), strings.TrimSpace(it.ExpireAt), it.UpdatedAt, it.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNoExportWhitelist
	}
	return nil
}

func (d *DB) DeleteExportWhitelist(id string) error {
	res, err := d.Exec(`DELETE FROM tbl_export_whitelist WHERE id=?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNoExportWhitelist
	}
	return nil
}

// ExportWhitelistAllows 当前用户对该库是否有未过期的导出白名单。
func (d *DB) ExportWhitelistAllows(username, databaseID string, now time.Time) (bool, error) {
	username = strings.TrimSpace(username)
	databaseID = strings.TrimSpace(databaseID)
	if username == "" || databaseID == "" {
		return false, nil
	}
	rows, err := d.Query(`SELECT db_ids, expire_at FROM tbl_export_whitelist WHERE username=?`, username)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, expire string
		if err := rows.Scan(&raw, &expire); err != nil {
			return false, err
		}
		if exportWhitelistAllows(parseExportDBIDs(raw), expire, databaseID, now) {
			return true, nil
		}
	}
	return false, rows.Err()
}

var errNoExportWhitelist = errString("导出白名单不存在")

type errString string

func (e errString) Error() string { return string(e) }

func parseExportDBIDs(raw string) []string {
	var ids []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &ids); err != nil {
		return []string{}
	}
	return cleanDBIDs(ids)
}

func cleanDBIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func exportWhitelistAllows(dbIDs []string, expireAt, databaseID string, now time.Time) bool {
	databaseID = strings.TrimSpace(databaseID)
	if databaseID == "" {
		return false
	}
	hit := false
	for _, id := range dbIDs {
		if strings.TrimSpace(id) == databaseID {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	expireAt = strings.TrimSpace(expireAt)
	if expireAt == "" {
		return true
	}
	exp, err := time.Parse(time.RFC3339, expireAt)
	if err != nil {
		return false
	}
	return exp.After(now)
}
