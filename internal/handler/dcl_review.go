package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// canSubmitDCLTarget 权限申请的提交校验。实例或库未登记仍拒绝。
// 平台管理员在租户模式，或该库已对本租户可见时，不再把已登记实例写成未登记。
func (h *TicketHandler) canSubmitDCLTarget(c *gin.Context, instanceID, dbName, userID string) (bool, string, error) {
	tid := middleware.TenantID(c)
	if platformAdminAuditing(h.Store, c) {
		tid = ""
	}
	ok, deny, msg, err := h.canActOnTarget(tid, instanceID, dbName, userID)
	if err != nil || ok || tid == "" || deny != targetActDenyInstanceMissing {
		return ok, msg, err
	}
	visible, verr := h.dclDatabaseVisible(middleware.TenantID(c), userID, instanceID, dbName)
	if verr != nil {
		return false, "", verr
	}
	if visible {
		return true, "", nil
	}
	return false, msg, nil
}

// dclDatabaseVisible 与库列表一致：本租库、库授权，或域名/实例授权展开。
func (h *TicketHandler) dclDatabaseVisible(tenantID, userID, instanceID, dbName string) (bool, error) {
	if h == nil || h.Store == nil {
		return false, nil
	}
	inst, err := h.Store.GetInstance(instanceID)
	if err != nil || inst == nil {
		return false, err
	}
	dbEnt, err := h.Store.FindDatabase(instanceID, dbName)
	if err != nil || dbEnt == nil {
		return false, err
	}
	tid := strings.TrimSpace(tenantID)
	if tid == "" || strings.TrimSpace(dbEnt.TenantID) == tid || strings.TrimSpace(inst.TenantID) == tid {
		return true, nil
	}
	ok, err := h.Store.HasResourceGrant(tid, userID, "database", dbEnt.ID)
	if err != nil || ok {
		return ok, err
	}
	return h.Store.DatabaseVisibleViaInstanceAccess(tid, userID, dbEnt)
}

// resolveDCLReviewer 审批人沿用租户成员规则。登录名 admin 和超级审核白名单不必是资源所属租户成员。
func resolveDCLReviewer(db *store.DB, resourceTid, preferred string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" && dclReviewerBypassesTenant(db, preferred) {
		return preferred, nil
	}
	return resolveTenantAssignee(db, resourceTid, preferred)
}

func dclReviewerBypassesTenant(db *store.DB, name string) bool {
	name = strings.TrimSpace(name)
	if strings.EqualFold(name, "admin") {
		return true
	}
	return db != nil && db.IsGlobalApprover(name)
}

// canReviewDCLAcrossTenants admin、特权角色和超级审核白名单可以打开并批准其它租户的权限申请。
func (h *TicketHandler) canReviewDCLAcrossTenants(c *gin.Context) bool {
	if h == nil || h.Store == nil || c == nil {
		return false
	}
	user := strings.TrimSpace(middleware.Username(c))
	if user == "" {
		return false
	}
	if strings.EqualFold(user, "admin") || middleware.IsPrivileged(middleware.Role(c)) {
		return true
	}
	return h.Store.IsGlobalApprover(user)
}
