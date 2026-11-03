package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

const maxCDCAssignIDs = 200

// AssignTenant POST /cdc-sync/tasks/:id/assign-tenant — 连同子任务划拨到目标租户。
func (h *CDCHandler) AssignTenant(c *gin.Context) {
	to, ok := parseAssignTenantID(h.Store, c)
	if !ok {
		return
	}
	res, err := h.Store.AssignCDCTaskTenant(c.Param("id"), to)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	_ = h.Store.AppendTenantAudit(to, middleware.UserID(c), "resource.assign", "cdc_task:"+res.RootID)
	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"root_id": res.RootID,
		"moved":   res.Moved,
	})
}

// AssignTenantBatch POST /cdc-sync/tasks/assign-tenant — 批量划拨（按任务树去重）。
func (h *CDCHandler) AssignTenantBatch(c *gin.Context) {
	if !platformAdminAuditing(h.Store, c) {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅平台管理员在租户模式下可划拨资源"})
		return
	}
	var req struct {
		TenantID string   `json:"tenant_id"`
		IDs      []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	to := strings.TrimSpace(req.TenantID)
	if to == "" {
		to = middleware.TenantID(c)
	}
	if to == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "目标租户必填"})
		return
	}
	ids := uniqueTrimmedGrantIDs(req.IDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "ids 必填"})
		return
	}
	if len(ids) > maxCDCAssignIDs {
		c.JSON(http.StatusBadRequest, gin.H{"message": "单次最多划拨 200 个任务"})
		return
	}
	type failItem struct {
		ID      string `json:"id"`
		Name    string `json:"name,omitempty"`
		Message string `json:"message"`
	}
	okCount, moved := 0, 0
	failed := make([]failItem, 0)
	doneRoots := map[string]bool{}
	actor := middleware.UserID(c)
	for _, id := range ids {
		name := ""
		if t, _ := h.Store.GetCDC(id); t != nil {
			name = t.Name
		}
		res, err := h.Store.AssignCDCTaskTenant(id, to)
		if err != nil {
			failed = append(failed, failItem{ID: id, Name: name, Message: err.Error()})
			continue
		}
		okCount++
		moved += res.Moved
		if doneRoots[res.RootID] {
			continue
		}
		doneRoots[res.RootID] = true
		_ = h.Store.AppendTenantAudit(to, actor, "resource.assign", "cdc_task:"+res.RootID)
	}
	c.JSON(http.StatusOK, gin.H{
		"ok_count":   okCount,
		"fail_count": len(failed),
		"moved":      moved,
		"failed":     failed,
	})
}

// annotateCDCInstances 为任务附源/目标实例摘要（同一实例只查一次）。
func (h *CDCHandler) annotateCDCInstances(list []store.CDCTask) {
	cache := map[string]*store.InstanceBrief{}
	brief := func(id string) *store.InstanceBrief {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil
		}
		if b, ok := cache[id]; ok {
			return b
		}
		it, _ := h.Store.GetInstance(id)
		b := store.BriefOfInstance(it)
		cache[id] = b
		return b
	}
	for i := range list {
		list[i].SourceInstance = brief(list[i].SourceID)
		list[i].TargetInstance = brief(list[i].TargetID)
	}
}

// annotateCDCTenants 审计会话列表补归属租户。
func (h *CDCHandler) annotateCDCTenants(list []store.CDCTask) {
	if len(list) == 0 {
		return
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		ids = append(ids, list[i].ID)
	}
	tids, err := h.Store.CDCTaskTenantIDs(ids)
	if err != nil {
		return
	}
	names := tenantNameMap(h.Store)
	for i := range list {
		list[i].TenantID = tids[list[i].ID]
		applyTenantName(names, &list[i].TenantID, &list[i].TenantName)
	}
}
