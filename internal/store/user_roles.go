package store

import (
	"database/sql"
	"errors"
	"strings"
)

// ListUserRoleIDs 返回用户绑定的全部角色 ID（顺序不保证稳定，主角色见 users.role_id）。
func (d *DB) ListUserRoleIDs(userID string) ([]string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return []string{}, nil
	}
	rows, err := d.Query(`SELECT role_id FROM tbl_user_roles WHERE user_id=?`, userID)
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
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}

// ReplaceUserRoles 替换用户角色绑定；roleIDs 至少 1 个。同步主角色到 tbl_users.role_id/role。
func (d *DB) ReplaceUserRoles(userID string, roleIDs []string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return errors.New("user_id 必填")
	}
	seen := map[string]bool{}
	var cleaned []string
	for _, id := range roleIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		return errors.New("至少绑定一个角色")
	}
	for _, id := range cleaned {
		r, err := d.GetRole(id)
		if err != nil {
			return err
		}
		if r == nil {
			return errors.New("角色不存在: " + id)
		}
	}
	if _, err := d.Exec(`DELETE FROM tbl_user_roles WHERE user_id=?`, userID); err != nil {
		return err
	}
	for _, id := range cleaned {
		if _, err := d.Exec(`INSERT INTO tbl_user_roles(user_id, role_id) VALUES(?,?)`, userID, id); err != nil {
			return err
		}
	}
	primary := cleaned[0]
	r, err := d.GetRole(primary)
	if err != nil || r == nil {
		return err
	}
	_, err = d.Exec(`UPDATE tbl_users SET role_id=?, role=? WHERE id=?`, r.ID, r.Code, userID)
	return err
}

// AttachUserRoleIDs 填充 u.RoleIDs；若关联表为空则回退到主角色 role_id。
func (d *DB) AttachUserRoleIDs(u *User) {
	if u == nil || u.ID == "" {
		return
	}
	ids, err := d.ListUserRoleIDs(u.ID)
	if err != nil {
		return
	}
	if len(ids) == 0 && strings.TrimSpace(u.RoleID) != "" {
		ids = []string{u.RoleID}
	}
	u.RoleIDs = ids
}

func (d *DB) effectiveUserRoleIDs(userID string) ([]string, error) {
	ids, err := d.ListUserRoleIDs(userID)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		return ids, nil
	}
	var roleID sql.NullString
	err = d.QueryRow(`SELECT role_id FROM tbl_users WHERE id=?`, userID).Scan(&roleID)
	if err != nil {
		return []string{}, nil
	}
	if strings.TrimSpace(roleID.String) != "" {
		return []string{roleID.String}, nil
	}
	return []string{}, nil
}

func isAdminRoleCode(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "platform_admin", "super_admin", "平台管理员":
		return true
	default:
		return false
	}
}

// UserIsAdmin 任一绑定角色 code 为管理员。
func (d *DB) UserIsAdmin(userID string) (bool, error) {
	ids, err := d.effectiveUserRoleIDs(userID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		r, err := d.GetRole(id)
		if err != nil {
			return false, err
		}
		if r != nil && isAdminRoleCode(r.Code) {
			return true, nil
		}
	}
	var code string
	_ = d.QueryRow(`SELECT role FROM tbl_users WHERE id=?`, userID).Scan(&code)
	return isAdminRoleCode(code), nil
}

// UserHasURN 用户任一角色拥有该 URN，或用户为 admin。
func (d *DB) UserHasURN(userID, urn string) (bool, error) {
	ok, err := d.UserIsAdmin(userID)
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	ids, err := d.effectiveUserRoleIDs(userID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		has, err := d.RoleHasURN(id, urn)
		if err != nil {
			return false, err
		}
		if has {
			return true, nil
		}
	}
	return false, nil
}

// ListUserURNs 多角色 URN 并集；admin 返回全部启用菜单 URN。
func (d *DB) ListUserURNs(userID string) ([]string, error) {
	admin, err := d.UserIsAdmin(userID)
	if err != nil {
		return nil, err
	}
	if admin {
		all, err := d.ListMenus()
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var out []string
		for _, m := range all {
			u := strings.TrimSpace(m.URN)
			if u == "" || seen[u] || !m.Enabled {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
		if out == nil {
			out = []string{}
		}
		return out, nil
	}
	ids, err := d.effectiveUserRoleIDs(userID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, rid := range ids {
		list, err := d.ListRoleURNs(rid)
		if err != nil {
			return nil, err
		}
		for _, u := range list {
			if seen[u] {
				continue
			}
			seen[u] = true
			out = append(out, u)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// MenusForUser 多角色菜单并集树（启用菜单，skipEmpty 去掉无 path 叶子动作）。
func (d *DB) MenusForUser(userID string, enabledOnly bool) ([]MenuNode, error) {
	ids, err := d.effectiveUserRoleIDs(userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []MenuNode{}, nil
	}
	admin, err := d.UserIsAdmin(userID)
	if err != nil {
		return nil, err
	}
	if admin {
		return d.MenuTree(enabledOnly)
	}
	all, err := d.ListMenus()
	if err != nil {
		return nil, err
	}
	allow := map[string]bool{}
	for _, rid := range ids {
		mids, err := d.ListRoleMenuIDs(rid)
		if err != nil {
			return nil, err
		}
		for _, mid := range mids {
			allow[mid] = true
		}
	}
	var filtered []Menu
	for _, m := range all {
		if allow[m.ID] {
			filtered = append(filtered, m)
		}
	}
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
