package store

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (d *DB) ListRoles() ([]Role, error) {
	rows, err := d.Query(`
		SELECT r.id, r.name, r.code, r.description, r.created_at,
			COALESCE(NULLIF(TRIM(r.updated_at), ''), r.created_at) AS updated_at,
			COALESCE(m.cnt, 0) AS member_count
		FROM tbl_roles r
		LEFT JOIN (
			SELECT role_id, COUNT(DISTINCT user_id) AS cnt
			FROM tbl_user_roles
			GROUP BY role_id
		) m ON m.role_id = r.id
		ORDER BY r.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		var updated sql.NullString
		if err := rows.Scan(&r.ID, &r.Name, &r.Code, &r.Description, &r.CreatedAt, &updated, &r.MemberCount); err != nil {
			return nil, err
		}
		r.UpdatedAt = updated.String
		if r.UpdatedAt == "" {
			r.UpdatedAt = r.CreatedAt
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) GetRole(id string) (*Role, error) {
	r := &Role{}
	var updated sql.NullString
	err := d.QueryRow(`
		SELECT id, name, code, description, created_at,
			COALESCE(NULLIF(TRIM(updated_at), ''), created_at)
		FROM tbl_roles WHERE id=?`, id).
		Scan(&r.ID, &r.Name, &r.Code, &r.Description, &r.CreatedAt, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.UpdatedAt = updated.String
	if r.UpdatedAt == "" {
		r.UpdatedAt = r.CreatedAt
	}
	return r, nil
}

func (d *DB) GetRoleByCode(code string) (*Role, error) {
	r := &Role{}
	var updated sql.NullString
	err := d.QueryRow(`
		SELECT id, name, code, description, created_at,
			COALESCE(NULLIF(TRIM(updated_at), ''), created_at)
		FROM tbl_roles WHERE code=?`, code).
		Scan(&r.ID, &r.Name, &r.Code, &r.Description, &r.CreatedAt, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.UpdatedAt = updated.String
	if r.UpdatedAt == "" {
		r.UpdatedAt = r.CreatedAt
	}
	return r, nil
}

func (d *DB) CreateRole(r *Role) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	r.CreatedAt = now
	r.UpdatedAt = now
	_, err := d.Exec(`INSERT INTO tbl_roles(id, name, code, description, created_at, updated_at) VALUES(?,?,?,?,?,?)`,
		r.ID, r.Name, r.Code, r.Description, r.CreatedAt, r.UpdatedAt)
	return err
}

func (d *DB) UpdateRole(r *Role) error {
	r.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_roles SET name=?, code=?, description=?, updated_at=? WHERE id=?`,
		r.Name, r.Code, r.Description, r.UpdatedAt, r.ID)
	return err
}

// builtinRoleCodes 内置角色：不可删除；seed 的 GrantMenuToAllRoles 仅向这些角色灌菜单。
var builtinRoleCodes = map[string]bool{
	"admin": true, "operator": true, "viewer": true, "developer": true,
	"dba": true, "platform_admin": true, "super_admin": true,
}

// IsBuiltinRoleCode 内置角色判定（自定义角色如 pos_read 为 false）。
func IsBuiltinRoleCode(code string) bool {
	return builtinRoleCodes[strings.ToLower(strings.TrimSpace(code))]
}

func (d *DB) DeleteRole(id string) error {
	exist, err := d.GetRole(id)
	if err != nil {
		return err
	}
	if exist == nil {
		return errors.New("角色不存在")
	}
	if IsBuiltinRoleCode(exist.Code) {
		return errors.New("内置角色不可删除")
	}
	if _, err := d.Exec(`DELETE FROM tbl_role_menus WHERE role_id=?`, id); err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM tbl_user_roles WHERE role_id=?`, id); err != nil {
		return err
	}
	_, err = d.Exec(`DELETE FROM tbl_roles WHERE id=?`, id)
	return err
}

// ListRoleUsers 返回绑定该角色的用户简表。
func (d *DB) ListRoleUsers(roleID string) ([]User, error) {
	roleID = strings.TrimSpace(roleID)
	if roleID == "" {
		return []User{}, nil
	}
	rows, err := d.Query(`
		SELECT u.id, u.username, u.display_name, u.role, u.role_id, u.source, u.created_at
		FROM tbl_users u
		INNER JOIN tbl_user_roles ur ON ur.user_id = u.id
		WHERE ur.role_id = ?
		ORDER BY u.username`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var source sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.RoleID, &source, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Source = source.String
		if u.Source == "" {
			u.Source = UserSourcePlatform
		}
		out = append(out, u)
	}
	if out == nil {
		out = []User{}
	}
	return out, rows.Err()
}

const menuCols = `id, parent_id, title, urn, path, permission_desc, icon, sort_order, enabled, created_at, updated_at`

func (d *DB) ListMenus() ([]Menu, error) {
	if d != nil {
		d.menuCacheMu.RLock()
		if d.menuCache != nil && time.Since(d.menuCacheAt) < menuCacheTTL {
			out := append([]Menu(nil), d.menuCache...)
			d.menuCacheMu.RUnlock()
			return out, nil
		}
		d.menuCacheMu.RUnlock()
	}
	rows, err := d.Query(`SELECT ` + menuCols + ` FROM tbl_menus ORDER BY sort_order, title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Menu
	for rows.Next() {
		m, err := scanMenu(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if d != nil {
		d.menuCacheMu.Lock()
		d.menuCache = append([]Menu(nil), out...)
		d.menuCacheAt = time.Now()
		d.menuCacheMu.Unlock()
	}
	return out, nil
}

func (d *DB) invalidateMenuCache() {
	if d == nil {
		return
	}
	d.menuCacheMu.Lock()
	d.menuCache = nil
	d.menuCacheAt = time.Time{}
	d.menuCacheMu.Unlock()
}

func scanMenu(sc interface{ Scan(dest ...any) error }) (*Menu, error) {
	var m Menu
	var parent, path, urn, perm, icon sql.NullString
	var enabled int
	if err := sc.Scan(&m.ID, &parent, &m.Title, &urn, &path, &perm, &icon, &m.SortOrder, &enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	m.ParentID = parent.String
	m.URN = urn.String
	m.Path = path.String
	m.PermissionDesc = perm.String
	m.Icon = icon.String
	m.Enabled = enabled == 1
	return &m, nil
}

func buildMenuTree(list []Menu, parentID string, enabledOnly, skipEmpty bool) []MenuNode {
	byID := map[string]Menu{}
	for _, m := range list {
		byID[m.ID] = m
	}
	var nodes []MenuNode
	for _, m := range list {
		if m.ParentID != parentID {
			continue
		}
		if enabledOnly && !m.Enabled {
			continue
		}
		node := MenuNode{Menu: m, Children: buildMenuTree(list, m.ID, enabledOnly, skipEmpty)}
		if p, ok := byID[m.ParentID]; ok {
			node.ParentTitle = p.Title
		}
		// 只去掉操作节点；空目录保留，便于在菜单管理新建后先在侧栏看到
		if skipEmpty && m.Path == "" && len(node.Children) == 0 && IsActionMenu(m) {
			continue
		}
		nodes = append(nodes, node)
	}
	// 补全父目录时可能 append 到切片末尾，同级必须按 sort_order 排，避免侧栏一级顺序错乱
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].SortOrder != nodes[j].SortOrder {
			return nodes[i].SortOrder < nodes[j].SortOrder
		}
		return nodes[i].Title < nodes[j].Title
	})
	return nodes
}

func (d *DB) MenuTree(enabledOnly bool) ([]MenuNode, error) {
	list, err := d.ListMenus()
	if err != nil {
		return nil, err
	}
	return buildMenuTree(list, "", enabledOnly, enabledOnly), nil
}

// PageActionMenus 按页面 path 找到启用的页面菜单，返回其下启用的操作节点。
func (d *DB) PageActionMenus(path string) (*Menu, []Menu, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil, nil
	}
	list, err := d.ListMenus()
	if err != nil {
		return nil, nil, err
	}
	page, actions := pageActionMenus(list, path)
	return page, actions, nil
}

func pageActionMenus(list []Menu, path string) (*Menu, []Menu) {
	var page *Menu
	for i := range list {
		if list[i].Enabled && list[i].Path == path && !IsActionMenu(list[i]) {
			page = &list[i]
			break
		}
	}
	if page == nil {
		return nil, nil
	}
	var actions []Menu
	for _, m := range list {
		if m.ParentID == page.ID && m.Enabled && IsActionMenu(m) {
			actions = append(actions, m)
		}
	}
	return page, actions
}

func (d *DB) MenuAdminTree() ([]MenuNode, error) {
	list, err := d.ListMenus()
	if err != nil {
		return nil, err
	}
	return buildMenuTree(list, "", false, false), nil
}

func (d *DB) GetMenu(id string) (*Menu, error) {
	row := d.QueryRow(`SELECT `+menuCols+` FROM tbl_menus WHERE id=?`, id)
	m, err := scanMenu(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

func normalizeActionMenu(m *Menu) {
	if m == nil {
		return
	}
	if IsActionURN(m.URN) || (strings.TrimSpace(m.Path) == "" && strings.HasPrefix(m.ID, "m-act-")) {
		m.Path = ""
		if strings.TrimSpace(m.URN) == "" && m.ID != "" {
			m.URN = "um.action." + strings.TrimPrefix(m.ID, "m-act-")
		}
	}
}

func (d *DB) CreateMenu(m *Menu) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if m.ID == "" {
		m.ID = uuid.NewString()
	}
	m.CreatedAt, m.UpdatedAt = now, now
	en := 0
	if m.Enabled {
		en = 1
	}
	normalizeActionMenu(m)
	if strings.TrimSpace(m.URN) == "" {
		m.URN = defaultMenuURN(m.Path, m.ID)
	}
	normalizeActionMenu(m)
	if strings.TrimSpace(m.PermissionDesc) == "" {
		if IsActionURN(m.URN) {
			m.PermissionDesc = m.Title
		} else {
			m.PermissionDesc = "查看" + m.Title
		}
	}
	parent := nullable(m.ParentID)
	_, err := d.Exec(
		`INSERT INTO tbl_menus(id, parent_id, title, urn, path, permission_desc, icon, sort_order, enabled, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, parent, m.Title, nullable(m.URN), nullable(m.Path), nullable(m.PermissionDesc), nullable(m.Icon), m.SortOrder, en, m.CreatedAt, m.UpdatedAt,
	)
	if err == nil {
		d.invalidateMenuCache()
	}
	return err
}

func defaultMenuURN(path, id string) string {
	s := strings.Trim(path, "/")
	s = strings.ReplaceAll(s, "/", "-")
	if s == "" {
		s = strings.TrimPrefix(id, "m-")
	}
	if s == "" {
		s = uuid.NewString()[:8]
	}
	return "um.sidebar." + s
}

func (d *DB) UpdateMenu(m *Menu) error {
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	en := 0
	if m.Enabled {
		en = 1
	}
	normalizeActionMenu(m)
	if strings.TrimSpace(m.URN) == "" {
		m.URN = defaultMenuURN(m.Path, m.ID)
	}
	normalizeActionMenu(m)
	_, err := d.Exec(
		`UPDATE tbl_menus SET parent_id=?, title=?, urn=?, path=?, permission_desc=?, icon=?, sort_order=?, enabled=?, updated_at=? WHERE id=?`,
		nullable(m.ParentID), m.Title, nullable(m.URN), nullable(m.Path), nullable(m.PermissionDesc), nullable(m.Icon), m.SortOrder, en, m.UpdatedAt, m.ID,
	)
	if err == nil {
		d.invalidateMenuCache()
	}
	return err
}

func (d *DB) MoveMenu(id, dir string) error {
	cur, err := d.GetMenu(id)
	if err != nil || cur == nil {
		return err
	}
	all, err := d.ListMenus()
	if err != nil {
		return err
	}
	var sibs []Menu
	for _, m := range all {
		if m.ParentID == cur.ParentID {
			sibs = append(sibs, m)
		}
	}
	idx := -1
	for i, m := range sibs {
		if m.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	swap := idx - 1
	if dir == "down" {
		swap = idx + 1
	}
	if swap < 0 || swap >= len(sibs) {
		return nil
	}
	sibs[idx], sibs[swap] = sibs[swap], sibs[idx]
	for i := range sibs {
		sibs[i].SortOrder = (i + 1) * 10
		if err := d.UpdateMenu(&sibs[i]); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) DeleteMenu(id string) error {
	children, err := d.ListMenus()
	if err != nil {
		return err
	}
	ids := collectMenuIDs(children, id)
	ids = append(ids, id)
	for _, mid := range ids {
		if _, err := d.Exec(`DELETE FROM tbl_role_menus WHERE menu_id=?`, mid); err != nil {
			return err
		}
		if _, err := d.Exec(`DELETE FROM tbl_menus WHERE id=?`, mid); err != nil {
			return err
		}
	}
	d.invalidateMenuCache()
	return nil
}

func collectMenuIDs(list []Menu, parentID string) []string {
	var ids []string
	for _, m := range list {
		if m.ParentID == parentID {
			ids = append(ids, m.ID)
			ids = append(ids, collectMenuIDs(list, m.ID)...)
		}
	}
	return ids
}

func nullable(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func (d *DB) ListRoleMenuIDs(roleID string) ([]string, error) {
	rows, err := d.Query(`SELECT menu_id FROM tbl_role_menus WHERE role_id=?`, roleID)
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

func (d *DB) EnsureMenu(m *Menu) error {
	if m.ID == "" {
		return d.CreateMenu(m)
	}
	exist, err := d.GetMenu(m.ID)
	if err != nil {
		return err
	}
	if exist == nil {
		return d.CreateMenu(m)
	}
	exist.ParentID = m.ParentID
	exist.Title = m.Title
	if m.URN != "" {
		exist.URN = m.URN
	}
	exist.Path = m.Path
	if m.PermissionDesc != "" {
		exist.PermissionDesc = m.PermissionDesc
	}
	if m.Icon != "" {
		exist.Icon = m.Icon
	}
	exist.SortOrder = m.SortOrder
	exist.Enabled = true
	normalizeActionMenu(exist)
	return d.UpdateMenu(exist)
}

func (d *DB) GrantMenuFollowingParent(menuID, parentID string) error {
	if strings.TrimSpace(parentID) == "" {
		return d.GrantMenuToAllRoles(menuID)
	}
	roles, err := d.ListRoles()
	if err != nil {
		return err
	}
	for _, r := range roles {
		ids, err := d.ListRoleMenuIDs(r.ID)
		if err != nil {
			return err
		}
		hasParent, hasSelf := false, false
		for _, id := range ids {
			if id == parentID {
				hasParent = true
			}
			if id == menuID {
				hasSelf = true
			}
		}
		if !hasParent || hasSelf {
			continue
		}
		if _, err := d.Exec(`INSERT INTO tbl_role_menus(role_id, menu_id) VALUES(?,?)`, r.ID, menuID); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) GrantMenuToAllRoles(menuID string) error {
	roles, err := d.ListRoles()
	if err != nil {
		return err
	}
	for _, r := range roles {
		if !IsBuiltinRoleCode(r.Code) {
			continue
		}
		ids, err := d.ListRoleMenuIDs(r.ID)
		if err != nil {
			return err
		}
		found := false
		for _, id := range ids {
			if id == menuID {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if _, err := d.Exec(`INSERT INTO tbl_role_menus(role_id, menu_id) VALUES(?,?)`, r.ID, menuID); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ReplaceRoleMenus(roleID string, menuIDs []string) error {
	expanded, err := d.ExpandMenuIDsWithParents(menuIDs)
	if err != nil {
		return err
	}
	if _, err := d.Exec(`DELETE FROM tbl_role_menus WHERE role_id=?`, roleID); err != nil {
		return err
	}
	for _, mid := range expanded {
		if _, err := d.Exec(`INSERT INTO tbl_role_menus(role_id, menu_id) VALUES(?,?)`, roleID, mid); err != nil {
			return err
		}
	}
	_, err = d.Exec(`UPDATE tbl_roles SET updated_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), roleID)
	return err
}

func (d *DB) MenusForRole(roleID string, enabledOnly bool) ([]MenuNode, error) {
	all, err := d.ListMenus()
	if err != nil {
		return nil, err
	}
	ids, err := d.ListRoleMenuIDs(roleID)
	if err != nil {
		return nil, err
	}
	allow := map[string]bool{}
	for _, id := range ids {
		allow[id] = true
	}
	var filtered []Menu
	for _, m := range all {
		if allow[m.ID] {
			filtered = append(filtered, m)
		}
	}
	// keep parent groups if any child is allowed
	have := map[string]bool{}
	for _, m := range filtered {
		have[m.ID] = true
	}
	byID := map[string]Menu{}
	for _, m := range all {
		byID[m.ID] = m
	}
	for _, m := range filtered {
		pid := m.ParentID
		for pid != "" && !have[pid] {
			if p, ok := byID[pid]; ok {
				filtered = append(filtered, p)
				have[pid] = true
				pid = p.ParentID
			} else {
				break
			}
		}
	}
	return buildMenuTree(filtered, "", enabledOnly, true), nil
}

func (d *DB) CreateUser(u *User, passwordHash string) error {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	u.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(u.Source) == "" {
		u.Source = UserSourcePlatform
	}
	roleIDs := append([]string{}, u.RoleIDs...)
	if len(roleIDs) == 0 && strings.TrimSpace(u.RoleID) != "" {
		roleIDs = []string{u.RoleID}
	}
	if len(roleIDs) > 0 {
		primary := roleIDs[0]
		if r, _ := d.GetRole(primary); r != nil {
			u.RoleID = r.ID
			u.Role = r.Code
		}
	}
	_, err := d.Exec(
		`INSERT INTO tbl_users(id, username, display_name, role, role_id, password_hash, source, created_at) VALUES(?,?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.DisplayName, u.Role, u.RoleID, passwordHash, u.Source, u.CreatedAt,
	)
	if err != nil {
		return err
	}
	if len(roleIDs) > 0 {
		if err := d.ReplaceUserRoles(u.ID, roleIDs); err != nil {
			return err
		}
		u.RoleIDs = roleIDs
	}
	if email := strings.TrimSpace(u.Email); email != "" {
		if err := d.SetUserEmail(u.ID, email); err != nil {
			return err
		}
	}
	return d.EnsureDefaultMembership(u.ID, roleIDs)
}

func (d *DB) UpdateUserRole(id, roleID, roleCode, displayName string) error {
	if err := d.ReplaceUserRoles(id, []string{roleID}); err != nil {
		return err
	}
	_, err := d.Exec(`UPDATE tbl_users SET display_name=? WHERE id=?`, displayName, id)
	return err
}

func (d *DB) UpdateUserRoles(id string, roleIDs []string, displayName string) error {
	if strings.TrimSpace(displayName) != "" {
		if _, err := d.Exec(`UPDATE tbl_users SET display_name=? WHERE id=?`, displayName, id); err != nil {
			return err
		}
	}
	return d.ReplaceUserRoles(id, roleIDs)
}

func (d *DB) UpdateUserPassword(id, passwordHash string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_users SET password_hash=?, password_changed_at=? WHERE id=?`, passwordHash, now, id)
	return err
}

// MarkPasswordChanged 目录改密不写本地哈希时，仍记下改密时间以便踢掉旧登录。
func (d *DB) MarkPasswordChanged(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("id required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(`UPDATE tbl_users SET password_changed_at=? WHERE id=?`, now, id)
	return err
}

// UpdateUserUsername 修改登录用户名（用于 SSO 从 sub 迁到 nickname）。
func (d *DB) UpdateUserUsername(id, username string) error {
	username = strings.TrimSpace(username)
	if id == "" || username == "" {
		return errors.New("id and username required")
	}
	_, err := d.Exec(`UPDATE tbl_users SET username=? WHERE id=?`, username, id)
	return err
}

// UpdateUserDisplayName 仅更新显示名。
func (d *DB) UpdateUserDisplayName(id, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if id == "" || displayName == "" {
		return errors.New("id and display_name required")
	}
	_, err := d.Exec(`UPDATE tbl_users SET display_name=? WHERE id=?`, displayName, id)
	return err
}
