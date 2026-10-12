package store

import (
	"strings"
)

// UserAccess 登录/Me 一次装配结果：最多 1 次 ListMenus。
type UserAccess struct {
	Menus               []MenuNode
	Permissions         []string
	PlatformPermissions []string
}

// BootstrapUserAccess 用一次 ListMenus 产出菜单树、租户 URN、平台 URN。
// impersonate=true 时菜单为全量启用树，permissions 为用户全局 URN（审计代配）。
func (d *DB) BootstrapUserAccess(userID, tenantID string, enabledOnly, impersonate bool) (*UserAccess, error) {
	return d.BootstrapUserAccessFromUser(nil, userID, tenantID, enabledOnly, impersonate)
}

// BootstrapUserAccessFromUser 同 BootstrapUserAccess；若 u.Role 已是管理员则跳过多次角色查询。
func (d *DB) BootstrapUserAccessFromUser(u *User, userID, tenantID string, enabledOnly, impersonate bool) (*UserAccess, error) {
	if u != nil && strings.TrimSpace(userID) == "" {
		userID = u.ID
	}
	userID = strings.TrimSpace(userID)
	tenantID = strings.TrimSpace(tenantID)
	out := &UserAccess{
		Menus:               []MenuNode{},
		Permissions:         []string{},
		PlatformPermissions: []string{},
	}
	if userID == "" {
		return out, nil
	}

	all, err := d.ListMenus()
	if err != nil {
		return nil, err
	}

	// 用户表 role 已是 admin：直接全量菜单，避免 UserIsAdmin/TenantAdmin 的多次 RTT
	if u != nil && isAdminRoleCode(u.Role) && !impersonate {
		out.Menus = buildMenuTree(all, "", enabledOnly, enabledOnly)
		out.Permissions = enabledMenuURNs(all)
		out.PlatformPermissions = platformURNsFromMenus(all)
		return out, nil
	}

	platAdmin, err := d.UserIsPlatformAdmin(userID)
	if err != nil {
		return nil, err
	}
	if platAdmin && !impersonate {
		out.Menus = buildMenuTree(all, "", enabledOnly, enabledOnly)
		out.Permissions = enabledMenuURNs(all)
		out.PlatformPermissions = platformURNsFromMenus(all)
		return out, nil
	}

	if impersonate {
		out.Menus = buildMenuTree(all, "", enabledOnly, enabledOnly)
		perms, err := d.listUserURNsWithMenus(userID, all, platAdmin)
		if err != nil {
			return nil, err
		}
		out.Permissions = perms
		out.PlatformPermissions = filterPlatformURNs(perms)
		if len(out.PlatformPermissions) == 0 && platAdmin {
			out.PlatformPermissions = platformURNsFromMenus(all)
		}
		return out, nil
	}

	if tenantID == "" {
		menus, perms, err := d.menusAndURNsGlobal(userID, all, enabledOnly, platAdmin)
		if err != nil {
			return nil, err
		}
		out.Menus = menus
		out.Permissions = perms
		if platAdmin {
			out.PlatformPermissions = platformURNsFromMenus(all)
		} else {
			out.PlatformPermissions = filterPlatformURNs(perms)
		}
		return out, nil
	}

	tenantAdmin, err := d.UserIsTenantAdmin(userID, tenantID)
	if err != nil {
		return nil, err
	}
	if tenantAdmin {
		out.Menus = buildMenuTree(all, "", enabledOnly, enabledOnly)
		out.Permissions = enabledMenuURNs(all)
	} else {
		ids, err := d.effectiveTenantRoleIDs(userID, tenantID)
		if err != nil {
			return nil, err
		}
		out.Menus, err = d.menusFromRoleIDs(all, ids, enabledOnly)
		if err != nil {
			return nil, err
		}
		out.Permissions, err = d.urnsFromRoleIDs(ids)
		if err != nil {
			return nil, err
		}
	}

	if platAdmin {
		out.PlatformPermissions = platformURNsFromMenus(all)
	} else {
		globalURNs, err := d.listUserURNsWithMenus(userID, all, false)
		if err != nil {
			return nil, err
		}
		out.PlatformPermissions = filterPlatformURNs(globalURNs)
	}
	return out, nil
}

func enabledMenuURNs(all []Menu) []string {
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
	return out
}

func platformURNsFromMenus(all []Menu) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range all {
		u := strings.TrimSpace(m.URN)
		if u == "" || !m.Enabled || !IsPlatformURN(u) || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func filterPlatformURNs(urns []string) []string {
	var out []string
	for _, u := range urns {
		if IsPlatformURN(u) {
			out = append(out, u)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func (d *DB) listUserURNsWithMenus(userID string, all []Menu, knownAdmin bool) ([]string, error) {
	admin := knownAdmin
	if !admin {
		var err error
		admin, err = d.UserIsAdmin(userID)
		if err != nil {
			return nil, err
		}
	}
	if admin {
		return enabledMenuURNs(all), nil
	}
	ids, err := d.effectiveUserRoleIDs(userID)
	if err != nil {
		return nil, err
	}
	return d.urnsFromRoleIDs(ids)
}

func (d *DB) menusAndURNsGlobal(userID string, all []Menu, enabledOnly, platAdmin bool) ([]MenuNode, []string, error) {
	if platAdmin {
		return buildMenuTree(all, "", enabledOnly, enabledOnly), enabledMenuURNs(all), nil
	}
	ids, err := d.effectiveUserRoleIDs(userID)
	if err != nil {
		return nil, nil, err
	}
	if len(ids) == 0 {
		return []MenuNode{}, []string{}, nil
	}
	admin, err := d.UserIsAdmin(userID)
	if err != nil {
		return nil, nil, err
	}
	if admin {
		return buildMenuTree(all, "", enabledOnly, enabledOnly), enabledMenuURNs(all), nil
	}
	menus, err := d.menusFromRoleIDs(all, ids, enabledOnly)
	if err != nil {
		return nil, nil, err
	}
	urns, err := d.urnsFromRoleIDs(ids)
	if err != nil {
		return nil, nil, err
	}
	return menus, urns, nil
}

func (d *DB) menusFromRoleIDs(all []Menu, roleIDs []string, enabledOnly bool) ([]MenuNode, error) {
	if len(roleIDs) == 0 {
		return []MenuNode{}, nil
	}
	allow := map[string]bool{}
	for _, rid := range roleIDs {
		mids, err := d.ListRoleMenuIDs(rid)
		if err != nil {
			return nil, err
		}
		for _, mid := range mids {
			allow[mid] = true
		}
	}
	return filterMenuTreeByAllow(all, allow, enabledOnly), nil
}

func (d *DB) urnsFromRoleIDs(roleIDs []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, rid := range roleIDs {
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

func filterMenuTreeByAllow(all []Menu, allow map[string]bool, enabledOnly bool) []MenuNode {
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
	return buildMenuTree(filtered, "", enabledOnly, true)
}
