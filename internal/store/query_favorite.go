package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type QueryFavorite struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	DatabaseID string `json:"database_id,omitempty"`
	DBName    string `json:"db_name,omitempty"`
	SQL       string `json:"sql"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (d *DB) ensureQueryFavoritesTable() error {
	_, err := d.Exec(`CREATE TABLE IF NOT EXISTS tbl_query_favorites (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '',
		database_id TEXT NOT NULL DEFAULT '',
		db_name TEXT NOT NULL DEFAULT '',
		sql_text TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return err
	}
	_, _ = d.Exec(`CREATE INDEX IF NOT EXISTS idx_query_fav_user ON tbl_query_favorites(username)`)
	return nil
}

func (d *DB) ListQueryFavorites(username string, limit int) ([]QueryFavorite, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := d.Query(`SELECT id, user_id, username, name, database_id, db_name, sql_text, created_at, updated_at
		FROM tbl_query_favorites WHERE username=? ORDER BY updated_at DESC LIMIT ?`, username, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueryFavorite
	for rows.Next() {
		var it QueryFavorite
		if err := rows.Scan(&it.ID, &it.UserID, &it.Username, &it.Name, &it.DatabaseID, &it.DBName, &it.SQL, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if out == nil {
		out = []QueryFavorite{}
	}
	return out, rows.Err()
}

func (d *DB) CreateQueryFavorite(it *QueryFavorite) error {
	if it == nil {
		return fmt.Errorf("nil favorite")
	}
	if strings.TrimSpace(it.ID) == "" {
		it.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if it.CreatedAt == "" {
		it.CreatedAt = now
	}
	it.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_query_favorites(id, user_id, username, name, database_id, db_name, sql_text, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		it.ID, it.UserID, it.Username, it.Name, it.DatabaseID, it.DBName, it.SQL, it.CreatedAt, it.UpdatedAt)
	return err
}

func (d *DB) DeleteQueryFavorite(id, username string) error {
	res, err := d.Exec(`DELETE FROM tbl_query_favorites WHERE id=? AND username=?`, strings.TrimSpace(id), strings.TrimSpace(username))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *DB) ListMyQueryAudits(username string, pageSize int) ([]QueryAudit, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := d.Query(`SELECT id, kind, user_id, username, display_name, ip, user_agent,
		instance_id, instance_name, db_type, target, statement,
		success, message, row_count, elapsed_ms, created_at
		FROM tbl_query_audits WHERE username=? AND kind='db' ORDER BY created_at DESC LIMIT ?`, username, pageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []QueryAudit{}
	for rows.Next() {
		var it QueryAudit
		var success int
		if err := rows.Scan(&it.ID, &it.Kind, &it.UserID, &it.Username, &it.DisplayName, &it.IP, &it.UserAgent,
			&it.InstanceID, &it.InstanceName, &it.DBType, &it.Target, &it.Statement,
			&success, &it.Message, &it.RowCount, &it.ElapsedMS, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.Success = success != 0
		out = append(out, it)
	}
	return out, rows.Err()
}
