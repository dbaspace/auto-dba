package store

import (
	"strings"
)

// IsPlatformURN 平台级权限（不依赖租户上下文）。
func IsPlatformURN(urn string) bool {
	urn = strings.TrimSpace(urn)
	return strings.HasPrefix(urn, "um.action.tenants.") ||
		urn == "um.sidebar.tenants" ||
		urn == URNMenusCreate || urn == URNMenusEdit || urn == URNMenusDelete ||
		urn == URNUsersCreate || urn == URNUsersEdit || urn == URNUsersDelete ||
		urn == URNLdapEdit
}

// UserIsPlatformAdmin 平台管理员：可管租户生命周期，默认不自动拥有各租户业务数据。
func (d *DB) UserIsPlatformAdmin(userID string) (bool, error) {
	return d.UserIsAdmin(userID)
}

// effectiveTenantRoleIDs 取用户在租户内的角色；无成员则空。
func (d *DB) effectiveTenantRoleIDs(userID, tenantID string) ([]string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return d.effectiveUserRoleIDs(userID)
	}
	m, err := d.GetTenantMember(tenantID, userID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.Status != MemberStatusActive {
		return []string{}, nil
	}
	return append([]string{}, m.RoleIDs...), nil
}

// UserIsTenantAdmin 租户内 admin 角色。
func (d *DB) UserIsTenantAdmin(userID, tenantID string) (bool, error) {
	ids, err := d.effectiveTenantRoleIDs(userID, tenantID)
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
	return false, nil
}

// UserHasURNInTenant 租户上下文下校验 URN；平台 URN 走全局角色。
func (d *DB) UserHasURNInTenant(userID, tenantID, urn string) (bool, error) {
	urn = strings.TrimSpace(urn)
	if urn == "" {
		return false, nil
	}
	if IsPlatformURN(urn) {
		return d.UserHasURN(userID, urn)
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return d.UserHasURN(userID, urn)
	}
	ok, err := d.UserIsTenantAdmin(userID, tenantID)
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	ids, err := d.effectiveTenantRoleIDs(userID, tenantID)
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

// ListUserURNsInTenant 租户内 URN 并集；租户 admin 返回全部启用菜单 URN。
func (d *DB) ListUserURNsInTenant(userID, tenantID string) ([]string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return d.ListUserURNs(userID)
	}
	admin, err := d.UserIsTenantAdmin(userID, tenantID)
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
	ids, err := d.effectiveTenantRoleIDs(userID, tenantID)
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

// MenusForUserInTenant 租户上下文菜单树。
func (d *DB) MenusForUserInTenant(userID, tenantID string, enabledOnly bool) ([]MenuNode, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return d.MenusForUser(userID, enabledOnly)
	}
	admin, err := d.UserIsTenantAdmin(userID, tenantID)
	if err != nil {
		return nil, err
	}
	if admin {
		return d.MenuTree(enabledOnly)
	}
	ids, err := d.effectiveTenantRoleIDs(userID, tenantID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []MenuNode{}, nil
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

// ListPlatformURNs 平台级 URN（全局角色）。
func (d *DB) ListPlatformURNs(userID string) ([]string, error) {
	all, err := d.ListUserURNs(userID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, u := range all {
		if IsPlatformURN(u) {
			out = append(out, u)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}
