package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func stampTenantID(c *gin.Context, set func(string)) {
	if set == nil || c == nil {
		return
	}
	if tid := middleware.TenantID(c); tid != "" {
		set(tid)
	}
}

func requireTenantID(c *gin.Context) (string, bool) {
	tid := middleware.TenantID(c)
	if tid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择租户"})
		return "", false
	}
	return tid, true
}

// platformAdminAuditing 平台管理员处于租户审计（impersonate）会话。
func platformAdminAuditing(db *store.DB, c *gin.Context) bool {
	if db == nil || c == nil || !middleware.IsImpersonating(c) {
		return false
	}
	ok, err := db.UserIsPlatformAdmin(middleware.UserID(c))
	return err == nil && ok
}

// effectiveListTenantID 列表过滤用租户：审计会话返回空串（appendTenantFilter 不过滤 = 全平台）。
func effectiveListTenantID(db *store.DB, c *gin.Context) string {
	if platformAdminAuditing(db, c) {
		return ""
	}
	return middleware.TenantID(c)
}

func auditScope(db *store.DB, c *gin.Context) bool {
	return platformAdminAuditing(db, c)
}

// listScopeTenant：查询参数 scope=tenant 时强制按当前租户列表（含 grant），即使平台管理员审计会话也不返回全平台。
func listScopeTenant(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(c.Query("scope")), "tenant")
}

func tenantNameMap(db *store.DB) map[string]string {
	out := map[string]string{}
	if db == nil {
		return out
	}
	list, err := db.ListTenants()
	if err != nil {
		return out
	}
	for _, t := range list {
		n := t.Name
		if n == "" {
			n = t.Code
		}
		out[t.ID] = n
	}
	return out
}

func applyTenantName(names map[string]string, tenantID, tenantName *string) {
	if tenantID == nil || tenantName == nil {
		return
	}
	if n := names[strings.TrimSpace(*tenantID)]; n != "" {
		*tenantName = n
	}
}

// parseAssignTenantID 仅平台审计可划拨；body.tenant_id 空则用当前 JWT tid。
func parseAssignTenantID(db *store.DB, c *gin.Context) (string, bool) {
	if !platformAdminAuditing(db, c) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅平台管理员在租户模式下可划拨资源"})
		return "", false
	}
	var req struct {
		TenantID string `json:"tenant_id"`
	}
	_ = c.ShouldBindJSON(&req)
	to := strings.TrimSpace(req.TenantID)
	if to == "" {
		to = middleware.TenantID(c)
	}
	if to == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "目标租户必填"})
		return "", false
	}
	return to, true
}

func resolveInstanceInTenant(db *store.DB, c *gin.Context, instanceID string) (*store.Instance, error) {
	if platformAdminAuditing(db, c) {
		return db.GetInstance(instanceID)
	}
	tid := middleware.TenantID(c)
	it, err := db.GetInstanceInTenant(instanceID, tid)
	if err != nil || it != nil {
		return it, err
	}
	ok, err := db.HasResourceGrant(tid, middleware.UserID(c), "instance", instanceID)
	if err != nil {
		return nil, err
	}
	if ok {
		return db.GetInstance(instanceID)
	}
	n, err := db.CountDatabasesOnInstanceInTenant(instanceID, tid)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return db.GetInstance(instanceID)
	}
	viaDomain, err := db.InstanceReachableViaAccessDomain(instanceID, tid, middleware.UserID(c))
	if err != nil {
		return nil, err
	}
	if viaDomain {
		return db.GetInstance(instanceID)
	}
	granted, err := db.ListGrantedDatabases(tid, middleware.UserID(c), instanceID, "")
	if err != nil || len(granted) == 0 {
		return nil, err
	}
	return db.GetInstance(instanceID)
}

func resolveParentInstance(db *store.DB, c *gin.Context, rec *store.Database) (*store.Instance, error) {
	if rec == nil {
		return nil, nil
	}
	if platformAdminAuditing(db, c) {
		return db.GetInstance(rec.InstanceID)
	}
	tid := middleware.TenantID(c)
	if strings.TrimSpace(rec.TenantID) == tid {
		return db.GetInstance(rec.InstanceID)
	}
	ok, err := db.HasResourceGrant(tid, middleware.UserID(c), "database", rec.ID)
	if err != nil {
		return nil, err
	}
	if ok {
		return db.GetInstance(rec.InstanceID)
	}
	return resolveInstanceInTenant(db, c, rec.InstanceID)
}

func resolveSrClusterInTenant(db *store.DB, c *gin.Context, id string) (*store.SrCluster, error) {
	if platformAdminAuditing(db, c) {
		return db.GetSrCluster(id)
	}
	tid := middleware.TenantID(c)
	cl, err := db.GetSrClusterInTenant(id, tid)
	if err != nil || cl != nil {
		return cl, err
	}
	ok, err := db.HasResourceGrant(tid, middleware.UserID(c), "sr_cluster", id)
	if err != nil || !ok {
		return nil, err
	}
	return db.GetSrCluster(id)
}

func resolveDbDeployClusterInTenant(db *store.DB, c *gin.Context, id string) (*store.DbDeployCluster, error) {
	if platformAdminAuditing(db, c) {
		return db.GetDbDeployCluster(id)
	}
	tid := middleware.TenantID(c)
	cl, err := db.GetDbDeployClusterInTenant(id, tid)
	if err != nil || cl != nil {
		return cl, err
	}
	ok, err := db.HasResourceGrant(tid, middleware.UserID(c), "db_deploy_cluster", id)
	if err != nil || !ok {
		return nil, err
	}
	return db.GetDbDeployCluster(id)
}

// tenantInstanceIDSet 当前租户可见实例 ID 集合；无租户上下文或平台审计时返回 nil（不过滤）。
func tenantInstanceIDSet(db *store.DB, c *gin.Context) map[string]bool {
	if db == nil || platformAdminAuditing(db, c) {
		return nil
	}
	tid := middleware.TenantID(c)
	if tid == "" {
		return nil
	}
	list, err := db.ListInstancesInTenant(tid, "", "")
	if err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(list))
	for i := range list {
		out[list[i].ID] = true
	}
	ids, err := db.ListInstanceIDsForTenantDatabases(tid)
	if err == nil {
		for _, id := range ids {
			out[id] = true
		}
	}
	if granted, gerr := grantedVisibleInstanceIDs(db, tid, middleware.UserID(c)); gerr == nil {
		for _, id := range granted {
			out[id] = true
		}
	}
	return out
}
