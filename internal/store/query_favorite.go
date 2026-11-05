package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type QueryFavorite struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	DatabaseID string `json:"database_id,omitempty"`
	DBName     string `json:"db_name,omitempty"`
	Env        string `json:"env,omitempty"`
	SQL        string `json:"sql"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
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
	if err := d.ensureColumnIfMissing("tbl_query_favorites", "env", "TEXT NOT NULL DEFAULT ''"); err != nil {
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
	rows, err := d.Query(`SELECT id, user_id, username, name, database_id, db_name, COALESCE(env,''), sql_text, created_at, updated_at
		FROM tbl_query_favorites WHERE username=? ORDER BY updated_at DESC LIMIT ?`, username, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueryFavorite
	for rows.Next() {
		var it QueryFavorite
		if err := rows.Scan(&it.ID, &it.UserID, &it.Username, &it.Name, &it.DatabaseID, &it.DBName, &it.Env, &it.SQL, &it.CreatedAt, &it.UpdatedAt); err != nil {
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
	if err := d.fillFavoriteEnv(it); err != nil {
		return err
	}
	_, err := d.Exec(`INSERT INTO tbl_query_favorites(id, user_id, username, name, database_id, db_name, env, sql_text, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.UserID, it.Username, it.Name, it.DatabaseID, it.DBName, it.Env, it.SQL, it.CreatedAt, it.UpdatedAt)
	return err
}

func (d *DB) UpdateQueryFavorite(it *QueryFavorite) error {
	if it == nil || strings.TrimSpace(it.ID) == "" || strings.TrimSpace(it.Username) == "" {
		return sql.ErrNoRows
	}
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := d.fillFavoriteEnv(it); err != nil {
		return err
	}
	res, err := d.Exec(`UPDATE tbl_query_favorites SET name=?, database_id=?, db_name=?, env=?, sql_text=?, updated_at=? WHERE id=? AND username=?`,
		it.Name, it.DatabaseID, it.DBName, it.Env, it.SQL, it.UpdatedAt, strings.TrimSpace(it.ID), strings.TrimSpace(it.Username))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
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

// fillFavoriteEnv 收藏未带环境时，从已登记库的 idc 标签或实例 idc 补上。已有环境则不改。
func (d *DB) fillFavoriteEnv(it *QueryFavorite) error {
	if it == nil || strings.TrimSpace(it.Env) != "" {
		return nil
	}
	id := strings.TrimSpace(it.DatabaseID)
	if id == "" {
		return nil
	}
	db, err := d.GetDatabase(id)
	if err != nil || db == nil {
		return err
	}
	if strings.TrimSpace(it.DBName) == "" {
		it.DBName = db.DBName
	}
	env := catalogTag(db.Tags, "idc")
	if env == "" && strings.TrimSpace(db.InstanceID) != "" {
		inst, ierr := d.GetInstance(db.InstanceID)
		if ierr != nil {
			return ierr
		}
		if inst != nil {
			env = strings.TrimSpace(inst.IDC)
		}
	}
	it.Env = env
	return nil
}

// BackfillQueryFavoriteDatabaseIDs 给旧收藏补上唯一匹配的已登记库 id。同名多条且环境对不上时跳过。
func (d *DB) BackfillQueryFavoriteDatabaseIDs() (int, error) {
	rows, err := d.Query(`SELECT id, COALESCE(database_id,''), COALESCE(db_name,''), COALESCE(env,'') FROM tbl_query_favorites`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type favRow struct {
		id, databaseID, dbName, env string
	}
	var pending []favRow
	for rows.Next() {
		var row favRow
		if err := rows.Scan(&row.id, &row.databaseID, &row.dbName, &row.env); err != nil {
			return 0, err
		}
		if strings.TrimSpace(row.databaseID) == "" && strings.TrimSpace(row.dbName) != "" {
			pending = append(pending, row)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, row := range pending {
		matches, err := d.listCatalogRows(` FROM tbl_databases d
			JOIN tbl_instances i ON i.id = d.instance_id
			WHERE d.db_name=?`, strings.TrimSpace(row.dbName))
		if err != nil {
			return n, err
		}
		wantEnv := strings.TrimSpace(row.env)
		var hit *catalogRow
		for i := range matches {
			env := catalogTag(matches[i].db.Tags, "idc")
			if env == "" {
				env = strings.TrimSpace(matches[i].inst.IDC)
			}
			if wantEnv != "" && env != wantEnv {
				continue
			}
			if hit != nil {
				hit = nil
				break
			}
			hit = &matches[i]
		}
		if hit == nil {
			continue
		}
		if _, err := d.Exec(`UPDATE tbl_query_favorites SET database_id=? WHERE id=? AND COALESCE(database_id,'')=''`, hit.db.ID, row.id); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func catalogTag(tags []string, key string) string {
	for _, raw := range tags {
		i := strings.Index(raw, "=")
		if i <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(raw[:i]), key) {
			return strings.TrimSpace(raw[i+1:])
		}
	}
	return ""
}
