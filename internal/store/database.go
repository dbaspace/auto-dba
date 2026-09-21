package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const databaseCols = `id, instance_id, db_name, approvers, visibility, access_users, role_ids, tags, domains, created_at, updated_at`

func normalizeDatabase(it *Database) {
	if it.Approvers == nil {
		it.Approvers = []string{}
	}
	it.Approvers = uniqueTrimmed(it.Approvers)
	it.AccessUsers = uniqueTrimmed(it.AccessUsers)
	it.RoleIDs = uniqueTrimmed(it.RoleIDs)
	it.Tags = uniqueTrimmed(it.Tags)
	it.Domains = uniqueTrimmed(it.Domains)
	if strings.EqualFold(strings.TrimSpace(it.Visibility), "private") {
		it.Visibility = "private"
	} else {
		it.Visibility = "global"
	}
}

func scanDatabase(scanner interface {
	Scan(dest ...any) error
}) (*Database, error) {
	var it Database
	var approvers, visibility, accessUsers, roleIDs, tags, domains sql.NullString
	if err := scanner.Scan(
		&it.ID, &it.InstanceID, &it.DBName, &approvers, &visibility, &accessUsers, &roleIDs, &tags, &domains, &it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return nil, err
	}
	it.Approvers = decodeStringSlice(approvers.String)
	it.AccessUsers = decodeStringSlice(accessUsers.String)
	it.RoleIDs = decodeStringSlice(roleIDs.String)
	it.Tags = decodeStringSlice(tags.String)
	it.Domains = decodeStringSlice(domains.String)
	it.Visibility = strings.TrimSpace(visibility.String)
	if it.Visibility == "" {
		it.Visibility = "global"
	}
	return &it, nil
}

func (d *DB) ListDatabases(instanceID, q string) ([]Database, error) {
	query := `SELECT ` + databaseCols + ` FROM tbl_databases WHERE 1=1`
	var args []any
	if instanceID != "" {
		query += ` AND instance_id=?`
		args = append(args, instanceID)
	}
	if q != "" {
		like := "%" + q + "%"
		query += ` AND (db_name LIKE ? OR COALESCE(tags,'') LIKE ? OR COALESCE(domains,'') LIKE ?)`
		args = append(args, like, like, like)
	}
	query += ` ORDER BY db_name`
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Database
	for rows.Next() {
		it, err := scanDatabase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func (d *DB) GetDatabase(id string) (*Database, error) {
	it, err := scanDatabase(d.QueryRow(`SELECT `+databaseCols+` FROM tbl_databases WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) FindDatabase(instanceID, dbName string) (*Database, error) {
	it, err := scanDatabase(d.QueryRow(
		`SELECT `+databaseCols+` FROM tbl_databases WHERE instance_id=? AND db_name=?`,
		instanceID, dbName,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func (d *DB) CreateDatabases(tpl *Database, dbNames []string) (created, skipped []string, err error) {
	if tpl == nil {
		tpl = &Database{}
	}
	normalizeDatabase(tpl)
	seen := map[string]struct{}{}
	for _, name := range dbNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		exist, findErr := d.FindDatabase(tpl.InstanceID, name)
		if findErr != nil {
			return created, skipped, findErr
		}
		if exist != nil {
			skipped = append(skipped, name)
			continue
		}
		it := *tpl
		it.ID = ""
		it.DBName = name
		if err := d.CreateDatabase(&it); err != nil {
			return created, skipped, err
		}
		created = append(created, name)
	}
	return created, skipped, nil
}

func (d *DB) CreateDatabase(it *Database) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if it.ID == "" {
		it.ID = uuid.NewString()
	}
	it.CreatedAt, it.UpdatedAt = now, now
	normalizeDatabase(it)
	_, err := d.Exec(
		`INSERT INTO tbl_databases(`+databaseCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.InstanceID, it.DBName, encodeJSON(it.Approvers), it.Visibility,
		encodeJSON(it.AccessUsers), encodeJSON(it.RoleIDs), encodeJSON(it.Tags), encodeJSON(it.Domains), it.CreatedAt, it.UpdatedAt,
	)
	return err
}

func (d *DB) UpdateDatabase(it *Database) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	normalizeDatabase(it)
	_, err := d.Exec(
		`UPDATE tbl_databases SET db_name=?, approvers=?, visibility=?, access_users=?, role_ids=?, tags=?, domains=?, updated_at=? WHERE id=?`,
		it.DBName, encodeJSON(it.Approvers), it.Visibility, encodeJSON(it.AccessUsers), encodeJSON(it.RoleIDs), encodeJSON(it.Tags), encodeJSON(it.Domains), it.UpdatedAt, it.ID,
	)
	return err
}

func (d *DB) DeleteDatabase(id string) error {
	_, err := d.Exec(`DELETE FROM tbl_databases WHERE id=?`, id)
	return err
}

type Viewer struct {
	Username   string
	RoleID     string   // 主角色，兼容旧逻辑
	RoleIDs    []string // 全部角色，可见性按交集
	Privileged bool
}

func (d *DB) ViewerOf(username, jwtRole string) Viewer {
	v := Viewer{Username: strings.TrimSpace(username), Privileged: isPrivilegedRole(jwtRole), RoleIDs: []string{}}
	if v.Username == "" {
		return v
	}
	u, err := d.GetUserByUsername(v.Username)
	if err != nil || u == nil {
		return v
	}
	v.RoleID = u.RoleID
	v.RoleIDs = append([]string{}, u.RoleIDs...)
	if len(v.RoleIDs) == 0 && u.RoleID != "" {
		v.RoleIDs = []string{u.RoleID}
	}
	if isPrivilegedRole(u.Role) {
		v.Privileged = true
	}
	for _, rid := range v.RoleIDs {
		if r, _ := d.GetRole(rid); r != nil && isPrivilegedRole(r.Code) {
			v.Privileged = true
			break
		}
	}
	return v
}

func isPrivilegedRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "platform_admin", "super_admin", "operator", "dba", "运维", "平台管理员":
		return true
	default:
		return false
	}
}

func (it Database) VisibleTo(v Viewer) bool {
	if v.Privileged || !strings.EqualFold(it.Visibility, "private") {
		return true
	}
	for _, name := range it.AccessUsers {
		if strings.EqualFold(strings.TrimSpace(name), v.Username) {
			return true
		}
	}
	roleSet := map[string]bool{}
	for _, id := range v.RoleIDs {
		if id != "" {
			roleSet[id] = true
		}
	}
	if v.RoleID != "" {
		roleSet[v.RoleID] = true
	}
	if len(roleSet) == 0 {
		return false
	}
	for _, id := range it.RoleIDs {
		if roleSet[id] {
			return true
		}
	}
	return false
}

func FilterVisibleDatabases(list []Database, v Viewer) []Database {
	if list == nil {
		return []Database{}
	}
	out := make([]Database, 0, len(list))
	for _, it := range list {
		if it.VisibleTo(v) {
			out = append(out, it)
		}
	}
	return out
}
