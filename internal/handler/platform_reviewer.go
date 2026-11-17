package handler

import (
	"strings"

	"auto-dba/internal/store"
)

// platformChangeReviewer 与云 PostgreSQL 工单同一口径：平台管理员、dba 租户活跃成员、全局审核白名单。
func platformChangeReviewer(db *store.DB, username, userID string) bool {
	if db == nil {
		return false
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return false
	}
	u, _ := db.GetUserByUsername(username)
	if u != nil && u.IsLoginBlocked() {
		return false
	}
	if db.IsGlobalApprover(username) {
		return true
	}
	userID = strings.TrimSpace(userID)
	if userID == "" && u != nil {
		userID = u.ID
	}
	if userID == "" {
		return false
	}
	if ok, _ := db.UserIsPlatformAdmin(userID); ok {
		return true
	}
	if t, _ := db.GetTenantByCode(store.DBATenantCode); t != nil {
		if ok, _ := db.IsActiveTenantMember(t.ID, userID); ok {
			return true
		}
	}
	return false
}

// resolveRedisAssignee 填写了审核人且属于云 PG 同一审核池时，不要求当前租户成员。
// 其余人仍须是当前租户活跃成员；未填则用 sync_default_approver，同样须是当前租户成员。
func resolveRedisAssignee(db *store.DB, tenantID, preferred string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" && platformChangeReviewer(db, preferred, "") {
		u, err := db.GetUserByUsername(preferred)
		if err != nil {
			return "", err
		}
		if u == nil {
			u, err = db.GetUserByUsernameFold(preferred)
			if err != nil {
				return "", err
			}
		}
		if u != nil && strings.TrimSpace(u.Username) != "" {
			return strings.TrimSpace(u.Username), nil
		}
		return preferred, nil
	}
	return resolveTenantAssignee(db, tenantID, preferred)
}

// listPlatformChangeReviewers 下拉名单，顺序为平台管理员、白名单、dba 租户活跃成员。同名只保留先出现的。
func listPlatformChangeReviewers(db *store.DB) ([]string, error) {
	if db == nil {
		return nil, nil
	}
	users, err := db.ListUsers()
	if err != nil {
		return nil, err
	}
	var admins []string
	for i := range users {
		u := &users[i]
		if u.IsLoginBlocked() || strings.TrimSpace(u.Username) == "" {
			continue
		}
		ok, err := db.UserIsPlatformAdmin(u.ID)
		if err != nil {
			return nil, err
		}
		if ok && platformChangeReviewer(db, u.Username, u.ID) {
			admins = append(admins, u.Username)
		}
	}
	raw, err := db.GetGlobalArg(store.ArgIssueGlobalApproverWhitelist)
	if err != nil {
		return nil, err
	}
	var whitelist []string
	for _, name := range store.ParseApproverWhitelist(raw) {
		if platformChangeReviewer(db, name, "") {
			whitelist = append(whitelist, name)
		}
	}
	var dbaNames []string
	t, err := db.GetTenantByCode(store.DBATenantCode)
	if err != nil {
		return nil, err
	}
	if t != nil {
		members, err := listTenantMemberUsernames(db, t.ID)
		if err != nil {
			return nil, err
		}
		for _, name := range members {
			if platformChangeReviewer(db, name, "") {
				dbaNames = append(dbaNames, name)
			}
		}
	}
	return store.ExpandApproverNames(append(append(admins, whitelist...), dbaNames...)), nil
}
