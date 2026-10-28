package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func uniqueTrimmedNames(names []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, n)
	}
	return out
}

// requireTenantMemberUsernames 要求每个用户名均为该租户活跃成员。
func requireTenantMemberUsernames(db *store.DB, tenantID string, names []string) error {
	_, dropped, err := keepTenantMemberUsernames(db, tenantID, names)
	if err != nil {
		return err
	}
	if len(dropped) == 0 {
		return nil
	}
	return fmt.Errorf("用户 %s 不是该租户的活跃成员", dropped[0])
}

// keepTenantMemberUsernames 保留租户活跃成员，其余放入 dropped（用户不存在也算 dropped）。
func keepTenantMemberUsernames(db *store.DB, tenantID string, names []string) (kept, dropped []string, err error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, nil, fmt.Errorf("缺少租户上下文")
	}
	for _, name := range uniqueTrimmedNames(names) {
		u, e := db.GetUserByUsername(name)
		if e != nil {
			return nil, nil, e
		}
		if u == nil {
			dropped = append(dropped, name)
			continue
		}
		ok, e := db.IsActiveTenantMember(tenantID, u.ID)
		if e != nil {
			return nil, nil, e
		}
		if !ok {
			dropped = append(dropped, name)
			continue
		}
		kept = append(kept, name)
	}
	return kept, dropped, nil
}

// mergeDatabaseApprovers 追加审核人：保留现有合法成员 ∪ 本次合法成员；非成员进 dropped。
func mergeDatabaseApprovers(db *store.DB, tenantID string, existing, submitted []string) (merged, dropped []string, err error) {
	keptExist, dropExist, err := keepTenantMemberUsernames(db, tenantID, existing)
	if err != nil {
		return nil, nil, err
	}
	keptAdd, dropAdd, err := keepTenantMemberUsernames(db, tenantID, submitted)
	if err != nil {
		return nil, nil, err
	}
	dropped = uniqueTrimmedNames(append(dropExist, dropAdd...))
	merged = uniqueTrimmedNames(append(keptExist, keptAdd...))
	return merged, dropped, nil
}

// loadTenantMemberUsernameSet 一次加载租户活跃成员（小写用户名 → 规范用户名），供批量 merge 复用。
func loadTenantMemberUsernameSet(db *store.DB, tenantID string) (map[string]string, error) {
	names, err := listTenantMemberUsernames(db, tenantID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := out[key]; !ok {
			out[key] = name
		}
	}
	return out, nil
}

// filterNamesByMemberSet 按成员集合过滤；kept 使用集合内的规范用户名。
func filterNamesByMemberSet(names []string, members map[string]string) (kept, dropped []string) {
	for _, name := range uniqueTrimmedNames(names) {
		canon, ok := members[strings.ToLower(name)]
		if !ok {
			dropped = append(dropped, name)
			continue
		}
		kept = append(kept, canon)
	}
	return kept, dropped
}

// mergeDatabaseApproversWithSet 用预加载成员集做追加合并，避免每库重复查库。
func mergeDatabaseApproversWithSet(existing, submitted []string, members map[string]string) (merged, dropped, keptAdd []string) {
	keptExist, dropExist := filterNamesByMemberSet(existing, members)
	keptAdd, dropAdd := filterNamesByMemberSet(submitted, members)
	dropped = uniqueTrimmedNames(append(dropExist, dropAdd...))
	merged = uniqueTrimmedNames(append(keptExist, keptAdd...))
	return merged, dropped, keptAdd
}

// subtractApprovers 从现有名单减去待撤销用户（大小写不敏感）；不要求对方为本租户成员。
// merged 可为 empty；removed 为实际删掉的规范名；missing 为请求撤销但不在该库上的人。
func subtractApprovers(existing, remove []string) (merged, removed, missing []string) {
	removeSet := map[string]struct{}{}
	for _, n := range uniqueTrimmedNames(remove) {
		removeSet[strings.ToLower(n)] = struct{}{}
	}
	hit := map[string]struct{}{}
	for _, n := range uniqueTrimmedNames(existing) {
		key := strings.ToLower(n)
		if _, ok := removeSet[key]; ok {
			removed = append(removed, n)
			hit[key] = struct{}{}
			continue
		}
		merged = append(merged, n)
	}
	if merged == nil {
		merged = []string{}
	}
	for _, n := range uniqueTrimmedNames(remove) {
		key := strings.ToLower(n)
		if _, ok := hit[key]; !ok {
			missing = append(missing, n)
		}
	}
	return merged, removed, missing
}

// approversContainAll 判断 have 是否包含 need 中全部用户名（大小写不敏感）。
func approversContainAll(have, need []string) bool {
	set := map[string]struct{}{}
	for _, n := range have {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		set[strings.ToLower(n)] = struct{}{}
	}
	for _, n := range need {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := set[strings.ToLower(n)]; !ok {
			return false
		}
	}
	return true
}

// validateResourceACL 校验可见性 ACL 与审批人：用户须为资源租户成员；角色 ID 须存在。
func validateResourceACL(db *store.DB, tenantID, visibility string, accessUsers, roleIDs, approvers []string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("缺少资源所属租户")
	}
	vis := strings.ToLower(strings.TrimSpace(visibility))
	if vis != "private" {
		accessUsers = nil
		roleIDs = nil
	}
	if err := requireTenantMemberUsernames(db, tenantID, accessUsers); err != nil {
		return err
	}
	if err := requireTenantMemberUsernames(db, tenantID, approvers); err != nil {
		return err
	}
	for _, id := range uniqueTrimmedNames(roleIDs) {
		r, err := db.GetRole(id)
		if err != nil {
			return err
		}
		if r == nil {
			return fmt.Errorf("角色不存在: %s", id)
		}
	}
	return nil
}

// listTenantMemberUsernames 活跃成员用户名（保持 ListTenantMembers 顺序）。
func listTenantMemberUsernames(db *store.DB, tenantID string) ([]string, error) {
	members, err := db.ListTenantMembers(tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(members))
	for _, m := range members {
		if m.Status != store.MemberStatusActive {
			continue
		}
		if name := strings.TrimSpace(m.Username); name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}

// TenantACLOptions GET /tenant-acl-options — 业务 ACL/审批人候选项（租户成员 + 成员角色并集）。
func (h *TenantHandler) ACLOptions(c *gin.Context) {
	tid := strings.TrimSpace(c.Query("tenant_id"))
	if tid == "" {
		tid = middleware.TenantID(c)
	}
	if tid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return
	}
	uid := middleware.UserID(c)
	allowed := false
	if platformAdminAuditing(h.Store, c) {
		allowed = true
	} else if uid != "" {
		ok, err := h.Store.IsActiveTenantMember(tid, uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		allowed = ok
	}
	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"message": "无权查看该租户成员选项"})
		return
	}
	members, err := h.Store.ListTenantMembers(tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	users := make([]gin.H, 0, len(members))
	roleSeen := map[string]struct{}{}
	roleIDs := make([]string, 0)
	for _, m := range members {
		if m.Status != store.MemberStatusActive {
			continue
		}
		users = append(users, gin.H{"username": m.Username, "display_name": m.DisplayName})
		for _, rid := range m.RoleIDs {
			rid = strings.TrimSpace(rid)
			if rid == "" {
				continue
			}
			if _, ok := roleSeen[rid]; ok {
				continue
			}
			roleSeen[rid] = struct{}{}
			roleIDs = append(roleIDs, rid)
		}
	}
	roles := make([]gin.H, 0)
	if len(roleIDs) == 0 {
		all, err := h.Store.ListRoles()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		for _, r := range all {
			roles = append(roles, gin.H{"id": r.ID, "name": r.Name, "code": r.Code})
		}
	} else {
		for _, rid := range roleIDs {
			r, err := h.Store.GetRole(rid)
			if err != nil || r == nil {
				continue
			}
			roles = append(roles, gin.H{"id": r.ID, "name": r.Name, "code": r.Code})
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"users": users, "roles": roles, "tenant_id": tid}})
}

func viewerForRequest(db *store.DB, c *gin.Context) func(tenantID string) store.Viewer {
	user := middleware.Username(c)
	role := middleware.Role(c)
	return func(tenantID string) store.Viewer {
		return db.ViewerOfInTenant(user, role, tenantID)
	}
}

// resolveTenantAssignee 解析审批人：优先 preferred，否则 sync_default_approver；均须为租户活跃成员。
func resolveTenantAssignee(db *store.DB, tenantID, preferred string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		if err := requireTenantMemberUsernames(db, tenantID, []string{preferred}); err != nil {
			return "", err
		}
		return preferred, nil
	}
	def, _ := db.GetGlobalArg("sync_default_approver")
	def = strings.TrimSpace(def)
	if def == "" {
		return "", fmt.Errorf("审批人必填，或配置全局参数 sync_default_approver（且须为本租户成员）")
	}
	if err := requireTenantMemberUsernames(db, tenantID, []string{def}); err != nil {
		return "", fmt.Errorf("默认审批人 %s 不是该租户成员，请显式指定审批人", def)
	}
	return def, nil
}
