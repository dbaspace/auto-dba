package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const databaseCols = `id, COALESCE(tenant_id,''), instance_id, db_name, approvers, visibility, access_users, role_ids, tags, domains, created_at, updated_at`
const databaseInsertCols = `id, instance_id, db_name, approvers, visibility, access_users, role_ids, tags, domains, created_at, updated_at`

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
		&it.ID, &it.TenantID, &it.InstanceID, &it.DBName, &approvers, &visibility, &accessUsers, &roleIDs, &tags, &domains, &it.CreatedAt, &it.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if it.TenantID == "" {
		it.TenantID = DefaultTenantID
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
	return d.ListDatabasesInTenant("", instanceID, q)
}

func (d *DB) ListDatabasesInTenant(tenantID, instanceID, q string) ([]Database, error) {
	query := `SELECT ` + databaseCols + ` FROM tbl_databases WHERE 1=1`
	var args []any
	query, args = appendTenantFilter(query, args, tenantID)
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

func (d *DB) GetDatabaseInTenant(id, tenantID string) (*Database, error) {
	it, err := d.GetDatabase(id)
	return ownedInTenant(d, it, err, "tbl_databases", id, tenantID)
}

// GetDatabaseVisible 本租户拥有或被 grant 的库。
func (d *DB) GetDatabaseVisible(id, tenantID, userID string) (*Database, error) {
	it, err := d.GetDatabaseInTenant(id, tenantID)
	if err != nil || it != nil {
		return it, err
	}
	it, err = d.GetDatabase(id)
	if err != nil || it == nil {
		return it, err
	}
	ok, err := d.HasResourceGrant(tenantID, userID, "database", id)
	if err != nil || !ok {
		return nil, err
	}
	return it, nil
}

func (d *DB) CountDatabasesOnInstanceInTenant(instanceID, tenantID string) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM tbl_databases WHERE instance_id=? AND COALESCE(tenant_id,'')=?`,
		strings.TrimSpace(instanceID), strings.TrimSpace(tenantID)).Scan(&n)
	return n, err
}

func (d *DB) ListInstanceIDsForTenantDatabases(tenantID string) ([]string, error) {
	rows, err := d.Query(`SELECT DISTINCT instance_id FROM tbl_databases WHERE COALESCE(tenant_id,'')=? AND COALESCE(instance_id,'')<>''`,
		strings.TrimSpace(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (d *DB) ListGrantedDatabases(toTenantID, toUserID, instanceID, q string) ([]Database, error) {
	gs, err := d.ListTenantGrantsTo(toTenantID, toUserID)
	if err != nil {
		return nil, err
	}
	instanceID = strings.TrimSpace(instanceID)
	q = strings.TrimSpace(q)
	var out []Database
	seen := map[string]struct{}{}
	for _, g := range gs {
		if g.ResourceType != "database" {
			continue
		}
		if _, ok := seen[g.ResourceID]; ok {
			continue
		}
		it, err := d.GetDatabase(g.ResourceID)
		if err != nil || it == nil {
			continue
		}
		if instanceID != "" && it.InstanceID != instanceID {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(it.DBName), strings.ToLower(q)) {
			continue
		}
		seen[it.ID] = struct{}{}
		out = append(out, *it)
	}
	return out, nil
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
		`INSERT INTO tbl_databases(`+databaseInsertCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.InstanceID, it.DBName, encodeJSON(it.Approvers), it.Visibility,
		encodeJSON(it.AccessUsers), encodeJSON(it.RoleIDs), encodeJSON(it.Tags), encodeJSON(it.Domains), it.CreatedAt, it.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if it.TenantID == "" {
		it.TenantID = d.inheritTenantFromInstance(it.InstanceID)
	}
	return d.AssignTenantID("tbl_databases", it.ID, it.TenantID)
}

func (d *DB) UpdateDatabase(it *Database) error {
	it.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	normalizeDatabase(it)
	_, err := d.Exec(
		`UPDATE tbl_databases SET db_name=?, approvers=?, visibility=?, access_users=?, role_ids=?, tags=?, domains=?, updated_at=? WHERE id=?`,
		it.DBName, encodeJSON(it.Approvers), it.Visibility, encodeJSON(it.AccessUsers), encodeJSON(it.RoleIDs), encodeJSON(it.Tags), encodeJSON(it.Domains), it.UpdatedAt, it.ID,
	)
	if err == nil {
		d.MaybeSnapshotDatabase(it, "")
	}
	return err
}

// UpdateDatabaseApprovers 仅更新审核人列，避免批量路径全行覆盖其它字段。
func (d *DB) UpdateDatabaseApprovers(id string, approvers []string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("database id required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	approvers = uniqueTrimmed(approvers)
	if approvers == nil {
		approvers = []string{}
	}
	res, err := d.Exec(
		`UPDATE tbl_databases SET approvers=?, updated_at=? WHERE id=?`,
		encodeJSON(approvers), now, id,
	)
	if err != nil {
		return err
	}
	if n, raErr := res.RowsAffected(); raErr == nil && n == 0 {
		return errors.New("审核人未写入（影响行数为 0）")
	}
	if it, getErr := d.GetDatabase(id); getErr == nil && it != nil {
		d.MaybeSnapshotDatabase(it, "")
	}
	return nil
}

// EffectiveDatabaseTenantID 解析库归属：原始列优先；列空则继承实例；再 fallback（如 JWT）。
// 避免 scan 把空列填成 tenant-default 后，再也走不到实例继承。
func (d *DB) EffectiveDatabaseTenantID(databaseID, instanceID, scannedTenantID, fallback string) string {
	raw, err := d.lookupRowTenantID("tbl_databases", databaseID)
	if err == nil && raw != "" {
		return raw
	}
	if inst := d.inheritTenantFromInstance(instanceID); inst != "" {
		return inst
	}
	if s := strings.TrimSpace(scannedTenantID); s != "" && s != DefaultTenantID {
		return s
	}
	if s := strings.TrimSpace(scannedTenantID); s != "" {
		return s
	}
	return strings.TrimSpace(fallback)
}

func (d *DB) DeleteDatabaseInTenant(id, tenantID string) error {
	it, err := d.GetDatabaseInTenant(id, tenantID)
	if err != nil {
		return err
	}
	if it == nil {
		return nil
	}
	return d.DeleteDatabase(id)
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

// ViewerOf 兼容旧调用：无租户上下文时用平台用户角色（测用 / 兜底）。
func (d *DB) ViewerOf(username, jwtRole string) Viewer {
	return d.ViewerOfInTenant(username, jwtRole, "")
}

// ViewerOfInTenant 按租户成员角色构造可见性 Viewer；tenantID 空则回退平台用户角色。
// Privileged 仍看平台用户角色（运维旁路 private）；RoleIDs 用于 private 角色交集时看租户成员角色。
func (d *DB) ViewerOfInTenant(username, jwtRole, tenantID string) Viewer {
	v := Viewer{Username: strings.TrimSpace(username), Privileged: isPrivilegedRole(jwtRole), RoleIDs: []string{}}
	if v.Username == "" {
		return v
	}
	u, err := d.GetUserByUsername(v.Username)
	if err != nil || u == nil {
		return v
	}
	if isPrivilegedRole(u.Role) {
		v.Privileged = true
	}
	platformRoleIDs := append([]string{}, u.RoleIDs...)
	if len(platformRoleIDs) == 0 && u.RoleID != "" {
		platformRoleIDs = []string{u.RoleID}
	}
	for _, rid := range platformRoleIDs {
		if r, _ := d.GetRole(rid); r != nil && isPrivilegedRole(r.Code) {
			v.Privileged = true
			break
		}
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID != "" {
		ids, err := d.effectiveTenantRoleIDs(u.ID, tenantID)
		if err == nil {
			v.RoleIDs = append([]string{}, ids...)
		}
	} else {
		v.RoleIDs = append([]string{}, platformRoleIDs...)
	}
	if len(v.RoleIDs) > 0 {
		v.RoleID = v.RoleIDs[0]
	} else {
		v.RoleID = u.RoleID
	}
	return v
}

// FilterVisibleDatabasesByTenant 按每行 tenant_id 构造 Viewer 过滤（审计多租列表用）。
func FilterVisibleDatabasesByTenant(list []Database, viewerFor func(tenantID string) Viewer) []Database {
	if list == nil {
		return []Database{}
	}
	cache := map[string]Viewer{}
	out := make([]Database, 0, len(list))
	for _, it := range list {
		tid := strings.TrimSpace(it.TenantID)
		if tid == "" {
			tid = DefaultTenantID
		}
		v, ok := cache[tid]
		if !ok {
			v = viewerFor(tid)
			cache[tid] = v
		}
		if it.VisibleTo(v) {
			out = append(out, it)
		}
	}
	return out
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
