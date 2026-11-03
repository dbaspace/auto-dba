package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

const maxCDCAssignIDs = 200

// cdcAssignVisibilityWarning 目标租户看不到源/目标实例时给出提示（不阻断划拨）。
func (h *CDCHandler) cdcAssignVisibilityWarning(rootID, toTenantID string) string {
	root, _ := h.Store.GetCDC(rootID)
	if root == nil {
		return ""
	}
	var parts []string
	check := func(label, instanceID string) {
		if strings.TrimSpace(instanceID) == "" {
			return
		}
		ok, err := h.Store.InstanceVisibleToTenant(instanceID, toTenantID)
		if err != nil || ok {
			return
		}
		name := instanceID
		if it, _ := h.Store.GetInstance(instanceID); it != nil && strings.TrimSpace(it.Name) != "" {
			name = it.Name
		}
		parts = append(parts, label+" "+name)
	}
	check("源端实例", root.SourceID)
	check("目标实例", root.TargetID)
	if len(parts) == 0 {
		return ""
	}
	return "目标租户看不到" + strings.Join(parts, "、") + "，详情中实例名会显示为地址、可选表等接口会失败；请一并划拨或授权对应库/实例"
}

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
		"warning": h.cdcAssignVisibilityWarning(res.RootID, to),
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
	type warnItem struct {
		ID      string `json:"id"`
		Name    string `json:"name,omitempty"`
		Message string `json:"message"`
	}
	okCount, moved := 0, 0
	failed := make([]failItem, 0)
	warnings := make([]warnItem, 0)
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
		if w := h.cdcAssignVisibilityWarning(res.RootID, to); w != "" {
			warnings = append(warnings, warnItem{ID: res.RootID, Name: name, Message: w})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"ok_count":   okCount,
		"fail_count": len(failed),
		"moved":      moved,
		"failed":     failed,
		"warnings":   warnings,
	})
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
