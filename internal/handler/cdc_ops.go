package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func encodeCDCExtraJSON(text string, extra map[string]any) string {
	b, _ := json.Marshal(extra)
	blob := "-- auto-dba-cdc:" + string(b)
	text = strings.TrimSpace(text)
	if text == "" {
		return blob
	}
	return text + "\n" + blob
}

func (h *CDCHandler) InitList(c *gin.Context) {
	list, err := h.Store.ListCDCInTenant(effectiveListTenantID(h.Store, c), "", "init")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	out := make([]gin.H, 0)
	for _, t := range list {
		if t.Status != store.StatusRunning && t.Status != store.StatusPaused && t.Status != store.StatusInit {
			continue
		}
		if t.Status == store.StatusDeleted {
			continue
		}
		// 瘦身：不回完整 mappings/tables；表名需走 /synced-tables。description 仅保留展示小字段。
		norm := normalizeCDCDescription(t.Description)
		counts := cdcListTableCounts(parseCDCExtraMap(norm))
		desc := slimCDCDescriptionForList(norm)
		extra := cdc.ParseTaskExtra(desc)
		synced := counts["target"]
		if synced == 0 {
			synced = counts["source"]
		}
		out = append(out, gin.H{
			"id": t.ID, "name": t.Name, "status": t.Status, "task_type": t.TaskType,
			"source_instance_id": t.SourceID, "target_instance_id": t.TargetID,
			"source_db": t.SourceDB, "target_db": t.TargetDB,
			"synced_table_count": synced,
			"publication_name":  extra["publication_name"], "env": extra["env"],
			"description": desc,
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *CDCHandler) SyncedTables(c *gin.Context) {
	init, err := h.resolveInitTask(c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": extraCSVTables(init.Description)})
}

func (h *CDCHandler) SyncedTablesBatch(c *gin.Context) {
	var req struct {
		IDs []string `json:"parent_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	out := map[string][]string{}
	for _, id := range uniqueNonEmpty(req.IDs) {
		if t, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c)); t != nil {
			out[id] = extraCSVTables(t.Description)
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *CDCHandler) AvailableTables(c *gin.Context) {
	init, err := h.resolveInitTask(c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	inst, _ := resolveInstanceInTenant(h.Store, c, init.SourceID)
	if inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "源端实例不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	db, err := dbmig.OpenInstance(ctx, inst, h.Secret, init.SourceDB)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer db.Close()
	schema := cdc.ParseTaskExtra(init.Description)["source_schema"]
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=$1 AND table_type='BASE TABLE' ORDER BY table_name`, schema)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && n != "" {
			all = append(all, n)
		}
	}
	synced := map[string]struct{}{}
	for _, t := range extraCSVTables(init.Description) {
		synced[strings.ToLower(t)] = struct{}{}
	}
	var avail []string
	for _, t := range all {
		if strings.EqualFold(t, "debezium_signal") {
			continue
		}
		if _, ok := synced[strings.ToLower(t)]; ok {
			continue
		}
		avail = append(avail, t)
	}
	if avail == nil {
		avail = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": avail})
}

func (h *CDCHandler) Publication(c *gin.Context) {
	init, err := h.resolveInitTask(c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	extra := cdc.ParseTaskExtra(init.Description)
	pub := extra["publication_name"]
	inst, _ := resolveInstanceInTenant(h.Store, c, init.SourceID)
	live := []string{}
	msg := ""
	if inst != nil && pub != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
		defer cancel()
		if db, oerr := dbmig.OpenInstance(ctx, inst, h.Secret, init.SourceDB); oerr == nil {
			if names, lerr := cdc.ListPublicationTables(ctx, db, pub); lerr == nil {
				live = names
			} else {
				msg = lerr.Error()
			}
			db.Close()
		} else {
			msg = oerr.Error()
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"publication_name": pub, "slot_name": extra["slot_name"],
		"tables": live, "task_tables": extraCSVTables(init.Description),
		"message": msg,
	}})
}

func (h *CDCHandler) AddPublicationTables(c *gin.Context) {
	h.createChildFromPublication(c, "add_table")
}

func (h *CDCHandler) RemovePublicationTables(c *gin.Context) {
	h.createChildFromPublication(c, "remove_table")
}

func (h *CDCHandler) createChildFromPublication(c *gin.Context, taskType string) {
	parent, err := h.resolveInitTask(c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	var req struct {
		Tables   []string `json:"tables"`
		Reviewer string   `json:"reviewer"`
		Approver string   `json:"approver"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(uniqueNonEmpty(req.Tables)) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "tables 必填"})
		return
	}
	tables := uniqueNonEmpty(req.Tables)
	if taskType == "remove_table" {
		tables = cdc.FilterProtectedPublicationTables(tables)
		if len(tables) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"message": "不能移除 debezium_signal 等受保护表"})
			return
		}
	}
	if taskType == "remove_table" {
		remain := subtractFold(extraCSVTables(parent.Description), tables)
		if len(remain) == 0 {
			taskType = "delete_task"
		}
	}
	parentExtra := cdc.ParseTaskExtra(parent.Description)
	suffix := "加表"
	if taskType == "remove_table" {
		suffix = "移除表"
	}
	if taskType == "delete_task" {
		suffix = "删除"
	}
	extra := map[string]any{
		"tables": tables, "source_schema": parentExtra["source_schema"],
		"publication_name": parentExtra["publication_name"], "slot_name": parentExtra["slot_name"],
		"connector_name": parentExtra["connector_name"], "topic_prefix": parentExtra["topic_prefix"],
		"debezium_mode": parentExtra["debezium_mode"], "connector_id": parentExtra["connector_id"],
		"connect_base_url": parentExtra["connect_base_url"],
		"env":              parentExtra["env"], "auto_create_table": parentExtra["auto_create_table"] == "true",
	}
	child := store.CDCTask{
		Name: parent.Name + "_" + suffix, TaskType: taskType, ParentID: parent.ID,
		SourceID: parent.SourceID, TargetID: parent.TargetID, SourceDB: parent.SourceDB, TargetDB: parent.TargetDB,
		Description: encodeCDCExtraJSON("", extra),
	}
	if err := h.prepareCDCCreate(c, &child); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	created, err := h.persistCDCTask(c, &child, req.Reviewer, req.Approver)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": created, "operation": taskType})
}

func (h *CDCHandler) RequestDelete(c *gin.Context) {
	parent, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || parent == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	if parent.TaskType != "init" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅初始化任务支持删除审批"})
		return
	}
	var req struct {
		Reviewer string `json:"reviewer"`
		Approver string `json:"approver"`
	}
	_ = c.ShouldBindJSON(&req)
	extra := cdc.ParseTaskExtra(parent.Description)
	child := store.CDCTask{
		Name: parent.Name + "_删除", TaskType: "delete_task", ParentID: parent.ID,
		SourceID: parent.SourceID, TargetID: parent.TargetID, SourceDB: parent.SourceDB, TargetDB: parent.TargetDB,
		Description: encodeCDCExtraJSON("删除 CDC 任务并清理 Publication / Slot / Connector", map[string]any{
			"tables": extraCSVTables(parent.Description), "publication_name": extra["publication_name"],
			"slot_name": extra["slot_name"], "connector_name": extra["connector_name"],
			"env": extra["env"],
		}),
	}
	if err := h.prepareCDCCreate(c, &child); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	created, err := h.persistCDCTask(c, &child, req.Reviewer, req.Approver)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": created})
}
