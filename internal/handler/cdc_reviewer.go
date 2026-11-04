package handler

import (
	"fmt"
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

func cdcDefaultApproverNames(db *store.DB) []string {
	if db == nil {
		return nil
	}
	raw, _ := db.GetGlobalArg("sync_default_approver")
	return splitApproverNames(raw)
}

func splitApproverNames(raw string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\n' || r == '\t'
	}) {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cdcSuperReviewer 平台管理员、sync_default_approver、dba 租户活跃成员。不要求加入业务租户，也不受实例/库审批人名单拦截。
func (h *CDCHandler) cdcSuperReviewer(username string) bool {
	username = strings.TrimSpace(username)
	if username == "" || h == nil || h.Store == nil {
		return false
	}
	if u, _ := h.Store.GetUserByUsername(username); u != nil {
		if ok, _ := h.Store.UserIsPlatformAdmin(u.ID); ok {
			return true
		}
		if t, _ := h.Store.GetTenantByCode(store.DBATenantCode); t != nil {
			if ok, _ := h.Store.IsActiveTenantMember(t.ID, u.ID); ok {
				return true
			}
		}
	}
	return usernameInList(username, cdcDefaultApproverNames(h.Store))
}

// resolveCDCChildReviewer 加表/加列审核人：任务租户成员、已授权资源归属租户成员，或超级用户。
func (h *CDCHandler) resolveCDCChildReviewer(t *store.CDCTask, preferred string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if i := strings.IndexAny(preferred, ",，"); i >= 0 {
		preferred = strings.TrimSpace(preferred[:i])
	}
	if preferred == "" {
		names := cdcDefaultApproverNames(h.Store)
		if len(names) == 0 {
			return "", errCDCReviewerRequired()
		}
		preferred = names[0]
	}
	if h.cdcReviewerAllowed(t, preferred) {
		return preferred, nil
	}
	return "", errCDCReviewerNotMember(preferred)
}

func errCDCReviewerRequired() error {
	return fmt.Errorf("审批人必填，或配置全局参数 sync_default_approver（且须为本租户成员）")
}

func errCDCReviewerNotMember(name string) error {
	return fmt.Errorf("用户 %s 不是该租户的活跃成员", name)
}

func (h *CDCHandler) cdcReviewerAllowed(t *store.CDCTask, username string) bool {
	username = strings.TrimSpace(username)
	if username == "" || h == nil || h.Store == nil {
		return false
	}
	if h.cdcSuperReviewer(username) {
		return true
	}
	u, err := h.Store.GetUserByUsername(username)
	if err != nil || u == nil {
		return false
	}
	taskTid := ""
	if t != nil {
		taskTid = strings.TrimSpace(t.TenantID)
	}
	if taskTid != "" {
		if ok, _ := h.Store.IsActiveTenantMember(taskTid, u.ID); ok {
			return true
		}
	}
	return h.cdcReviewerOwnsGrantedSource(t, u.ID)
}

// cdcReviewerOwnsGrantedSource 审核人是源库/域名/实例归属租户的活跃成员，且该资源已授权给任务租户。
func (h *CDCHandler) cdcReviewerOwnsGrantedSource(t *store.CDCTask, userID string) bool {
	if t == nil || strings.TrimSpace(userID) == "" {
		return false
	}
	taskTid := strings.TrimSpace(t.TenantID)
	if taskTid == "" {
		return false
	}
	allow := func(ownerTid, resourceType, resourceID string) bool {
		ownerTid = strings.TrimSpace(ownerTid)
		if ownerTid == "" || ownerTid == taskTid {
			return false
		}
		ok, err := h.Store.IsActiveTenantMember(ownerTid, userID)
		if err != nil || !ok {
			return false
		}
		granted, err := h.Store.HasResourceGrant(taskTid, cdcGrantProbeUser, resourceType, resourceID)
		return err == nil && granted
	}
	if db, _ := h.Store.FindDatabase(t.SourceID, t.SourceDB); db != nil && allow(db.TenantID, "database", db.ID) {
		return true
	}
	if inst, _ := h.Store.GetInstance(t.SourceID); inst != nil && allow(inst.TenantID, "instance", inst.ID) {
		return true
	}
	ads, err := h.Store.ListAccessDomains("", "", t.SourceID)
	if err != nil {
		return false
	}
	for i := range ads {
		if strings.TrimSpace(ads[i].InstanceID) != strings.TrimSpace(t.SourceID) {
			continue
		}
		if allow(ads[i].TenantID, "domain", ads[i].ID) {
			return true
		}
	}
	return false
}
