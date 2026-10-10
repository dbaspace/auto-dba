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
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return fmt.Errorf("缺少租户上下文")
	}
	names = uniqueTrimmedNames(names)
	for _, name := range names {
		u, err := db.GetUserByUsername(name)
		if err != nil {
			return err
		}
		if u == nil {
			return fmt.Errorf("用户 %s 不存在或不属于该租户", name)
		}
		ok, err := db.IsActiveTenantMember(tenantID, u.ID)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("用户 %s 不是该租户的活跃成员", name)
		}
	}
	return nil
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
