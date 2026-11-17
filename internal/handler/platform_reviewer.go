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

// resolveRedisAssignee 填写了审核人且属于该 Redis 域名审核人或云 PG 同一审核池时，不要求当前租户成员。
// 其余人仍须是当前租户活跃成员；未填则用 sync_default_approver，同样须是当前租户成员。
func resolveRedisAssignee(db *store.DB, tenantID, preferred string, domainApprovers []string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if name, ok := matchedName(preferred, domainApprovers); ok {
		return name, nil
	}
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

// redisTransferCandidates 更换 Redis 审核人的名单：该域名审核人在前，再并上平台审核池。不按实例租户过滤。
func redisTransferCandidates(db *store.DB, instanceID, domain string) ([]string, error) {
	var domainApprovers []string
	domain = strings.TrimSpace(domain)
	instanceID = strings.TrimSpace(instanceID)
	if db != nil && domain != "" {
		ad, err := db.GetAccessDomainByDomainType(domain, "redis")
		if err != nil {
			return nil, err
		}
		if ad != nil && (instanceID == "" || strings.TrimSpace(ad.InstanceID) == instanceID) {
			domainApprovers = ad.Approvers
		}
	}
	pool, err := listPlatformChangeReviewers(db)
	if err != nil {
		return nil, err
	}
	return store.ExpandApproverNames(append(append([]string{}, domainApprovers...), pool...)), nil
}

func matchedName(username string, list []string) (string, bool) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", false
	}
	for _, v := range list {
		v = strings.TrimSpace(v)
		if v != "" && strings.EqualFold(v, username) {
			return v, true
		}
	}
	return "", false
}
