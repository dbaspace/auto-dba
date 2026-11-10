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

// mergeInstanceSourceHost 只把新域名登记到实例 Domains；实例 Host/Port 由同实例其它库共用，不能被单个任务改写。
func mergeInstanceSourceHost(inst *store.Instance, host string) bool {
	if inst == nil {
		return false
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.EqualFold(strings.TrimSpace(inst.Host), host) || strings.EqualFold(strings.TrimSpace(inst.DomainName), host) {
		return false
	}
	for _, d := range inst.Domains {
		if strings.EqualFold(strings.TrimSpace(d), host) {
			return false
		}
	}
	inst.Domains = append(inst.Domains, host)
	return true
}

func cdcEffectiveSourceHost(t *store.CDCTask, inst *store.Instance) string {
	if t == nil {
		return ""
	}
	host, _ := cdc.EffectiveSourceEndpoint(cdcExtraMap(t.Description), inst)
	return strings.TrimSpace(host)
}

// repairSourceSiblings 同实例、修复前生效地址相同的其它运行中初始化任务。
func repairSourceSiblings(root *store.CDCTask, list []store.CDCTask, inst *store.Instance, beforeHost string) []*store.CDCTask {
	var out []*store.CDCTask
	if root == nil || strings.TrimSpace(beforeHost) == "" {
		return out
	}
	for i := range list {
		if list[i].ID == root.ID || list[i].SourceID != root.SourceID {
			continue
		}
		if !strings.EqualFold(cdcEffectiveSourceHost(&list[i], inst), beforeHost) {
			continue
		}
		out = append(out, &list[i])
	}
	return out
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
		c.JSON(http.StatusBadRequest, gin.H{"message": "地址不能为空"})
		return
	}
	if req.Port <= 0 || req.Port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "端口无效"})
		return
	}
	inst, reason, err := h.resolveRepairSource(root, host)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": reason})
		return
	}
	rebindSource := strings.TrimSpace(root.SourceID) != strings.TrimSpace(inst.ID)
	repairOthers := req.RepairOthers != nil && *req.RepairOthers
	beforeHost := cdcEffectiveSourceHost(root, inst)
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

	var others []store.CDCTask
	if repairOthers {
		if others, err = h.Store.ListCDCInTenant(effectiveListTenantID(h.Store, c), store.StatusRunning, "init"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
	}
	targets := append([]*store.CDCTask{root}, repairSourceSiblings(root, others, inst, beforeHost)...)

	if mergeInstanceSourceHost(inst, host) {
		if err := h.Store.UpdateInstance(inst); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "登记实例域名失败: " + err.Error()})
			return
		}
	}
	if _, err := h.Store.SyncAccessDomainsFromInstance(inst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "同步访问域名失败: " + err.Error()})
		return
	}
	if rebindSource {
		for _, task := range targets {
			if task == nil || strings.TrimSpace(task.SourceID) == inst.ID {
				continue
			}
			if err := h.Store.UpdateCDCSourceID(task.ID, inst.ID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": "写回源端实例失败: " + err.Error()})
				return
			}
			task.SourceID = inst.ID
		}
	}

	op, ip := cdcRequestActor(c)
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

func (h *CDCHandler) resolveRepairSource(root *store.CDCTask, host string) (*store.Instance, string, error) {
	if root == nil {
		return nil, cdc.SourceUnboundMsg, nil
	}
	var bound *store.Instance
	if id := strings.TrimSpace(root.SourceID); id != "" {
		inst, err := h.Store.GetInstance(id)
		if err != nil {
			return nil, "", err
		}
		bound = inst
	}
	if bound != nil {
		return bound, "", nil
	}
	extra := cdcExtraMap(root.Description)
	eff, _ := cdc.EffectiveSourceEndpoint(extra, nil)
	addrs := []string{host, eff}
	cands, err := cdc.LookupSourceCandidates(h.Store.ListInstancesByAddr, addrs)
	if err != nil {
		return nil, "", err
	}
	inst, reason := cdc.PickSourceInstance(nil, cands, root.TenantID, addrs)
	return inst, reason, nil
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
