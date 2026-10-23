package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

func mergeInstanceSourceHost(inst *store.Instance, host string, port int) {
	if inst == nil {
		return
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return
	}
	inst.Host = host
	if port > 0 {
		inst.Port = port
	}
	if strings.EqualFold(strings.TrimSpace(inst.DomainName), host) {
		return
	}
	for _, d := range inst.Domains {
		if strings.EqualFold(strings.TrimSpace(d), host) {
			return
		}
	}
	inst.Domains = append(inst.Domains, host)
}

func (h *CDCHandler) RepairSource(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	root := h.cdcRootTask(t)
	if root.TaskType != "init" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请到初始化任务上修复源端"})
		return
	}
	if root.Status == store.StatusPending || root.ApprovalStatus == store.StatusPending {
		c.JSON(http.StatusBadRequest, gin.H{"message": "待审批任务不能修复源端"})
		return
	}
	var req struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		RepairOthers *bool  `json:"repair_others"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	host := strings.TrimSpace(req.Host)
	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "域名不能为空"})
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "端口无效"})
		return
	}
	inst, err := h.Store.GetInstance(root.SourceID)
	if err != nil || inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "源端实例不存在"})
		return
	}
	repairOthers := true
	if req.RepairOthers != nil {
		repairOthers = *req.RepairOthers
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	probe := *inst
	probe.Host = host
	probe.Port = req.Port
	db, err := dbmig.OpenInstance(ctx, &probe, h.Secret, root.SourceDB)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "新地址连通失败: " + err.Error()})
		return
	}
	db.Close()

	mergeInstanceSourceHost(inst, host, req.Port)
	if err := h.Store.UpdateInstance(inst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "更新实例地址失败: " + err.Error()})
		return
	}
	if _, err := h.Store.SyncAccessDomainsFromInstance(inst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "同步访问域名失败: " + err.Error()})
		return
	}

	op, ip := cdcRequestActor(c)
	targets := []*store.CDCTask{root}
	if repairOthers {
		list, err := h.Store.ListCDCInTenant(effectiveListTenantID(h.Store, c), store.StatusRunning, "init")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		for i := range list {
			if list[i].ID == root.ID || list[i].SourceID != root.SourceID {
				continue
			}
			targets = append(targets, &list[i])
		}
	}
	var warnings []string
	for _, task := range targets {
		if w := h.applySourceEndpoint(ctx, task, host, req.Port, op, ip); w != "" {
			warnings = append(warnings, task.Name+": "+w)
		}
	}
	msg := "已修复源端，并重启 Connector"
	if len(warnings) > 0 {
		msg = "源端地址已更新"
	}
	c.JSON(http.StatusOK, gin.H{"message": msg, "warning": strings.Join(warnings, "；")})
}

func (h *CDCHandler) applySourceEndpoint(ctx context.Context, t *store.CDCTask, host string, port int, operator, ip string) string {
	if t == nil {
		return ""
	}
	extra := cdcExtraMap(t.Description)
	names := h.orch().TaskConnectorNames(t)
	var warnings []string
	for _, name := range names {
		cfg, err := h.orch().ConnectorConfig(ctx, extra, name)
		if err != nil || len(cfg) == 0 {
			cfg = cdc.CurrentConfigFromExtra(extra)
		}
		if len(cfg) == 0 {
			warnings = append(warnings, name+" 无配置")
			continue
		}
		before := map[string]string{}
		for k, v := range cfg {
			before[k] = v
		}
		cdc.ApplySourceEndpoint(cfg, host, port)
		if _, err := cdc.SaveConnectorConfigVersionIfChanged(h.Store, t.ID, before, cdc.VersionSourceRepairSource, operator, ip); err != nil {
			warnings = append(warnings, name+" 写入版本失败: "+err.Error())
			continue
		}
		if err := h.orch().PutConnectorConfig(ctx, extra, name, cfg); err != nil {
			warnings = append(warnings, name+" 更新配置失败: "+err.Error())
			continue
		}
	}
	fields := map[string]string{
		"source_host": host,
		"source_port": strconv.Itoa(port),
	}
	stored := cdc.CurrentConfigFromExtra(extra)
	if len(stored) > 0 {
		cdc.ApplySourceEndpoint(stored, host, port)
		fields["debezium_config"] = cdc.MarshalConfigMap(stored)
	}
	if err := cdc.WriteTaskExtraFields(h.Store, t, fields); err != nil {
		warnings = append(warnings, "写回任务失败: "+err.Error())
	}
	_ = h.Store.InsertCDCLog(&store.CDCSyncLog{
		TaskID: t.ID, Level: "INFO", Step: "manual", Operator: operator,
		Message: "用户 " + operator + " 修复源端为 " + host + ":" + strconv.Itoa(port),
	})
	if t.Status == store.StatusPaused {
		warnings = append(warnings, "任务已暂停，恢复或手动重启后生效")
		return strings.Join(warnings, "；")
	}
	if len(names) == 0 {
		warnings = append(warnings, "没有可重启的 Connector")
		return strings.Join(warnings, "；")
	}
	if err := h.orch().RestartConnectorTasks(ctx, t, extra, names); err != nil {
		warnings = append(warnings, err.Error())
	}
	return strings.Join(warnings, "；")
}
