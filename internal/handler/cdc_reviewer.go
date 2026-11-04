package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// cdcGrantProbeUser 传给 HasResourceGrant 的占位用户，避免空 to_user_id 误匹配其它租户的授权。
const cdcGrantProbeUser = "\x00"

// loadVisibleCDCInit 加表/加列父任务：列表范围能看见，或当前租户是源库/域名/实例归属方且已授权给任务租户。
func (h *CDCHandler) loadVisibleCDCInit(c *gin.Context, id string) *store.CDCTask {
	id = strings.TrimSpace(id)
	if id == "" || h == nil || h.Store == nil {
		return nil
	}
	p, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c))
	if p == nil && effectiveListTenantID(h.Store, c) != "" {
		if got, _ := h.Store.GetCDC(id); got != nil && got.TaskType == "init" && h.cdcParentVisibleViaGrant(c, got) {
			p = got
		}
	}
	if p == nil {
		return nil
	}
	if strings.TrimSpace(p.TenantID) == "" {
		p.TenantID = h.Store.RowTenantID("tbl_cdc_tasks", p.ID)
	}
	return p
}

// cdcParentVisibleViaGrant 当前 JWT 租户拥有该任务的源库、域名或实例，且资源已 grant 给任务租户。
func (h *CDCHandler) cdcParentVisibleViaGrant(c *gin.Context, p *store.CDCTask) bool {
	if h == nil || h.Store == nil || p == nil || c == nil {
		return false
	}
	jwt := middleware.TenantID(c)
	uid := middleware.UserID(c)
	if jwt == "" || uid == "" {
		return false
	}
	member, err := h.Store.IsActiveTenantMember(jwt, uid)
	if err != nil || !member {
		return false
	}
	taskTid := strings.TrimSpace(p.TenantID)
	if taskTid == "" {
		taskTid = h.Store.RowTenantID("tbl_cdc_tasks", p.ID)
		p.TenantID = taskTid
	}
	if taskTid == "" || taskTid == jwt {
		return false
	}
	if db, _ := h.Store.FindDatabase(p.SourceID, p.SourceDB); db != nil && strings.TrimSpace(db.TenantID) == jwt {
		if ok, _ := h.Store.HasResourceGrant(taskTid, cdcGrantProbeUser, "database", db.ID); ok {
			return true
		}
	}
	if inst, _ := h.Store.GetInstance(p.SourceID); inst != nil && strings.TrimSpace(inst.TenantID) == jwt {
		if ok, _ := h.Store.HasResourceGrant(taskTid, cdcGrantProbeUser, "instance", inst.ID); ok {
			return true
		}
	}
	ads, err := h.Store.ListAccessDomainsInTenant(jwt, "", "", "")
	if err != nil {
		return false
	}
	for i := range ads {
		if strings.TrimSpace(ads[i].InstanceID) != strings.TrimSpace(p.SourceID) {
			continue
		}
		if ok, _ := h.Store.HasResourceGrant(taskTid, cdcGrantProbeUser, "domain", ads[i].ID); ok {
			return true
		}
	}
	return false
}

// cdcInitParentsViaGrant 加表选父任务：补上「源资源归属本租户且已授权给任务租户」的 init，不放开该租户的其它任务。
func (h *CDCHandler) cdcInitParentsViaGrant(c *gin.Context) []store.CDCTask {
	if h == nil || h.Store == nil || c == nil || platformAdminAuditing(h.Store, c) {
		return nil
	}
	jwt := middleware.TenantID(c)
	uid := middleware.UserID(c)
	if jwt == "" || uid == "" {
		return nil
	}
	member, err := h.Store.IsActiveTenantMember(jwt, uid)
	if err != nil || !member {
		return nil
	}
	grants, err := h.Store.ListTenantGrantsFrom(jwt)
	if err != nil || len(grants) == 0 {
		return nil
	}
	byTo := map[string][]store.TenantGrant{}
	for _, g := range grants {
		to := strings.TrimSpace(g.ToTenantID)
		if to == "" || to == jwt {
			continue
		}
		switch strings.TrimSpace(g.ResourceType) {
		case "database", "domain", "instance":
		default:
			continue
		}
		if ok, _ := h.Store.HasResourceGrant(to, cdcGrantProbeUser, g.ResourceType, g.ResourceID); !ok {
			continue
		}
		byTo[to] = append(byTo[to], g)
	}
	var out []store.CDCTask
	seen := map[string]struct{}{}
	for to, gs := range byTo {
		tasks, err := h.Store.ListCDCInTenant(to, "", "init")
		if err != nil {
			continue
		}
		for i := range tasks {
			t := tasks[i]
			if _, ok := seen[t.ID]; ok {
				continue
			}
			if !cdcTaskCoveredByGrants(h.Store, &t, gs) {
				continue
			}
			t.TenantID = to
			seen[t.ID] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

func cdcTaskCoveredByGrants(db *store.DB, t *store.CDCTask, gs []store.TenantGrant) bool {
	if db == nil || t == nil {
		return false
	}
	for _, g := range gs {
		switch strings.TrimSpace(g.ResourceType) {
		case "database":
			rec, _ := db.GetDatabase(g.ResourceID)
			if rec != nil && rec.InstanceID == t.SourceID && strings.EqualFold(strings.TrimSpace(rec.DBName), strings.TrimSpace(t.SourceDB)) {
				return true
			}
		case "instance":
			if strings.TrimSpace(g.ResourceID) == strings.TrimSpace(t.SourceID) {
				return true
			}
		case "domain":
			ad, _ := db.GetAccessDomain(g.ResourceID)
			if ad != nil && strings.TrimSpace(ad.InstanceID) == strings.TrimSpace(t.SourceID) {
				return true
			}
		}
	}
	return false
}

const cdcReviewerDenied = "CDC 审核人须为 admin、全局审核白名单或 dba 租户用户"

// cdcReviewerInPool 平台管理员、issue_global_approver_whitelist、dba 租户活跃成员。不要求加入 CDC 任务租户。
func (h *CDCHandler) cdcReviewerInPool(username string) bool {
	username = strings.TrimSpace(username)
	if username == "" || h == nil || h.Store == nil {
		return false
	}
	u, _ := h.Store.GetUserByUsername(username)
	if u != nil && u.IsLoginBlocked() {
		return false
	}
	if h.Store.IsGlobalApprover(username) {
		return true
	}
	if u == nil {
		return false
	}
	if ok, _ := h.Store.UserIsPlatformAdmin(u.ID); ok {
		return true
	}
	if t, _ := h.Store.GetTenantByCode(store.DBATenantCode); t != nil {
		if ok, _ := h.Store.IsActiveTenantMember(t.ID, u.ID); ok {
			return true
		}
	}
	return false
}

// cdcSuperReviewer 与审核人池相同：不受实例/库审批人名单拦截。
func (h *CDCHandler) cdcSuperReviewer(username string) bool {
	return h.cdcReviewerInPool(username)
}

// listCDCReviewerUsernames 下拉名单：平台管理员、全局审核白名单、dba 租户活跃成员。
func (h *CDCHandler) listCDCReviewerUsernames() ([]string, error) {
	if h == nil || h.Store == nil {
		return nil, nil
	}
	var groups [][]string
	users, err := h.Store.ListUsers()
	if err != nil {
		return nil, err
	}
	var admins []string
	for i := range users {
		u := &users[i]
		if u.IsLoginBlocked() || strings.TrimSpace(u.Username) == "" {
			continue
		}
		ok, err := h.Store.UserIsPlatformAdmin(u.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			admins = append(admins, u.Username)
		}
	}
	groups = append(groups, admins)
	raw, err := h.Store.GetGlobalArg(store.ArgIssueGlobalApproverWhitelist)
	if err != nil {
		return nil, err
	}
	var whitelist []string
	for _, name := range store.ParseApproverWhitelist(raw) {
		if h.cdcReviewerInPool(name) {
			whitelist = append(whitelist, name)
		}
	}
	groups = append(groups, whitelist)
	tenants, err := h.Store.ListTenants()
	if err != nil {
		return nil, err
	}
	seenTID := map[string]struct{}{}
	for i := range tenants {
		t := &tenants[i]
		if !isDBATenant(t.ID, t.Code) {
			continue
		}
		if _, ok := seenTID[t.ID]; ok {
			continue
		}
		seenTID[t.ID] = struct{}{}
		names, err := listTenantMemberUsernames(h.Store, t.ID)
		if err != nil {
			return nil, err
		}
		var active []string
		for _, name := range names {
			if h.cdcReviewerInPool(name) {
				active = append(active, name)
			}
		}
		groups = append(groups, active)
	}
	return store.ExpandApproverNames(flattenNameGroups(groups...)), nil
}

func flattenNameGroups(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// Reviewers GET /cdc-sync/reviewers — CDC 建单审核人候选。
func (h *CDCHandler) Reviewers(c *gin.Context) {
	names, err := h.listCDCReviewerUsernames()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if names == nil {
		names = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": names})
}

// resolveCDCChildReviewer 只接受审核人池，不接受登记库/实例审批人，也不接受任务租户普通成员。
func (h *CDCHandler) resolveCDCChildReviewer(t *store.CDCTask, preferred string) (string, error) {
	_ = t
	preferred = strings.TrimSpace(preferred)
	if i := strings.IndexAny(preferred, ",，"); i >= 0 {
		preferred = strings.TrimSpace(preferred[:i])
	}
	if preferred == "" {
		names, err := h.listCDCReviewerUsernames()
		if err != nil {
			return "", err
		}
		if len(names) == 0 {
			return "", fmt.Errorf("%s", cdcReviewerDenied)
		}
		preferred = names[0]
	}
	if h.cdcReviewerInPool(preferred) {
		return preferred, nil
	}
	return "", fmt.Errorf("%s", cdcReviewerDenied)
}
