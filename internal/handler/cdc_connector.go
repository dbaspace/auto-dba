package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func cdcRequestActor(c *gin.Context) (operator, ip string) {
	operator = middleware.Username(c)
	ip = strings.TrimSpace(c.GetHeader("X-Real-IP"))
	if ip == "" {
		if xff := strings.TrimSpace(c.GetHeader("X-Forwarded-For")); xff != "" {
			ip = strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	if ip == "" {
		ip = c.ClientIP()
	}
	return operator, ip
}

func (h *CDCHandler) cdcRootTask(t *store.CDCTask) *store.CDCTask {
	if t == nil {
		return nil
	}
	id := cdc.VersionTaskID(t)
	if id == "" || id == t.ID {
		return t
	}
	// 同租户内解析父任务；跨租户则保持当前任务，避免越权读到其它租户 CDC。
	parent, err := h.Store.GetCDCInTenant(id, strings.TrimSpace(t.TenantID))
	if err != nil || parent == nil {
		return t
	}
	return parent
}

func (h *CDCHandler) loadCurrentConnectorConfig(ctx context.Context, t *store.CDCTask) map[string]string {
	extra := cdcExtraMap(t.Description)
	local := cdc.CurrentConfigFromExtra(extra)
	name := strings.TrimSpace(extra["connector_name"])
	if name == "" {
		return local
	}
	live, err := h.orch().ConnectorConfig(ctx, extra, name)
	if err != nil || len(live) == 0 {
		return local
	}
	return live
}

func decorateVersion(it *store.CDCConnectorConfigVersion) gin.H {
	if it == nil {
		return gin.H{}
	}
	cfg := cdc.ParseConfigMap(it.ConfigJSON)
	return gin.H{
		"id":               it.ID,
		"task_id":          it.TaskID,
		"version":          it.VersionNo,
		"version_no":       it.VersionNo,
		"change_source":    it.ChangeSource,
		"operation_id":     it.ChangeSource,
		"operator":         it.Operator,
		"operator_ip":      it.OperatorIP,
		"config_key_count": cdc.ConfigKeyCount(cfg),
		"created_at":       it.CreatedAt,
	}
}

func (h *CDCHandler) ConnectorUpdate(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	root := h.cdcRootTask(t)
	var req struct {
		Config map[string]string `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Config) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "config 必填"})
		return
	}
	if t.Status == store.StatusPending || t.ApprovalStatus == store.StatusPending {
		c.JSON(http.StatusBadRequest, gin.H{"message": "待审批任务不能修改 Connector 配置"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	if h.rejectIfConnectorMissing(c, ctx, root) {
		return
	}
	before := h.loadCurrentConnectorConfig(ctx, root)
	after := cdc.MergeConfigForUpdate(req.Config, before)
	if err := cdc.ValidateAndNormalizeConnectorCfg(after); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	op, ip := cdcRequestActor(c)
	if _, err := cdc.SaveConnectorConfigVersionIfChanged(h.Store, root.ID, before, cdc.VersionSourceUISave, op, ip); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入配置版本失败: " + err.Error()})
		return
	}
	extra := cdcExtraMap(root.Description)
	if name := strings.TrimSpace(extra["connector_name"]); name != "" {
		if err := h.orch().PutConnectorConfig(ctx, extra, name, after); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "更新 Connect 配置失败: " + err.Error()})
			return
		}
	}
	if err := cdc.WriteTaskDebeziumConfig(h.Store, root, after); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	resp := gin.H{"data": gin.H{
		"name": extra["connector_name"], "config": cdc.MaskConfigMap(after),
	}}
	if root.Status == store.StatusPaused {
		resp["restart_skipped"] = true
		resp["message"] = "已保存。任务已暂停，恢复或手动重启后生效"
		c.JSON(http.StatusOK, resp)
		return
	}
	names := h.orch().TaskConnectorNames(root)
	if err := h.orch().RestartConnectorTasks(ctx, root, extra, names); err != nil {
		resp["restart_error"] = err.Error()
		resp["message"] = "已保存。重启未完成：" + err.Error() + "。可再点详情里的「重启」"
		c.JSON(http.StatusOK, resp)
		return
	}
	resp["message"] = "已保存并已重启同步任务"
	c.JSON(http.StatusOK, resp)
}

func (h *CDCHandler) ConnectorVersionList(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	root := h.cdcRootTask(t)
	page, _ := strconv.Atoi(c.DefaultQuery("current", c.DefaultQuery("page", "1")))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", c.DefaultQuery("pageSize", "10")))
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	list, total, err := h.Store.ListCDCConnectorVersions(root.ID, size, (page-1)*size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(list))
	for i := range list {
		items = append(items, decorateVersion(&list[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"list":       items,
		"pagination": gin.H{"total": total, "current": page, "page_size": size},
	}})
}

func (h *CDCHandler) ConnectorVersionDetail(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	root := h.cdcRootTask(t)
	row, err := h.Store.GetCDCConnectorVersion(root.ID, c.Param("versionId"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置版本不存在"})
		return
	}
	item := decorateVersion(row)
	item["config"] = cdc.MaskConfigMap(cdc.ParseConfigMap(row.ConfigJSON))
	c.JSON(http.StatusOK, gin.H{"data": item})
}

func (h *CDCHandler) ConnectorVersionRestorePreview(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	root := h.cdcRootTask(t)
	row, err := h.Store.GetCDCConnectorVersion(root.ID, c.Param("versionId"))
	if err != nil || row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置版本不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	current := h.loadCurrentConnectorConfig(ctx, root)
	historical := cdc.ParseConfigMap(row.ConfigJSON)
	diff := cdc.DiffConfigMaps(current, historical)
	add, drop := cdc.TableIncludeDelta(current, historical)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"version":                row.VersionNo,
		"change_source":          row.ChangeSource,
		"connector_only":         len(add) == 0 && len(drop) == 0,
		"config_diff":            diff,
		"table_include_add":      add,
		"table_include_drop":     drop,
		"publication_add":        add,
		"publication_drop":       drop,
		"mappings_to_reactivate": add,
		"mappings_to_deactivate": drop,
		"topic_prefix_change":    strings.TrimSpace(current["topic.prefix"]) != strings.TrimSpace(historical["topic.prefix"]),
		"warnings":               []string{},
	}})
}

func (h *CDCHandler) ConnectorVersionRestore(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	root := h.cdcRootTask(t)
	row, err := h.Store.GetCDCConnectorVersion(root.ID, c.Param("versionId"))
	if err != nil || row == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "配置版本不存在"})
		return
	}
	var req struct {
		SyncPipeline *bool `json:"sync_pipeline"`
	}
	_ = c.ShouldBindJSON(&req)
	target := cdc.ParseConfigMap(row.ConfigJSON)
	if err := cdc.ValidateAndNormalizeConnectorCfg(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	if h.rejectIfConnectorMissing(c, ctx, root) {
		return
	}
	before := h.loadCurrentConnectorConfig(ctx, root)
	op, ip := cdcRequestActor(c)
	if _, err := cdc.SaveConnectorConfigVersionIfChanged(h.Store, root.ID, before, cdc.VersionSourceRestore, op, ip); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "写入配置版本失败: " + err.Error()})
		return
	}
	extra := cdcExtraMap(root.Description)
	if name := strings.TrimSpace(extra["connector_name"]); name != "" {
		if err := h.orch().PutConnectorConfig(ctx, extra, name, target); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "恢复到 Connect 失败: " + err.Error()})
			return
		}
	}
	if err := cdc.WriteTaskDebeziumConfig(h.Store, root, target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"name": extra["connector_name"], "config": cdc.MaskConfigMap(target),
		"restore_result": gin.H{"pipeline_synced": req.SyncPipeline == nil || *req.SyncPipeline},
	}})
}
