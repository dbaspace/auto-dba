package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
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
	envQ := strings.TrimSpace(c.Query("env"))
	nameQ := strings.ToLower(strings.TrimSpace(c.Query("q")))
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
		taskEnv := strings.TrimSpace(extra["env"])
		if envQ != "" && !strings.EqualFold(taskEnv, envQ) {
			continue
		}
		if nameQ != "" {
			name := strings.ToLower(t.Name)
			db := strings.ToLower(t.SourceDB)
			if !strings.Contains(name, nameQ) && !strings.Contains(db, nameQ) {
				continue
			}
		}
		synced := counts["target"]
		if synced == 0 {
			synced = counts["source"]
		}
		inst, _ := h.Store.GetInstance(t.SourceID)
		sourceHost, sourcePort := cdc.EffectiveSourceEndpoint(extra, inst)
		out = append(out, gin.H{
			"id": t.ID, "name": t.Name, "status": t.Status, "task_type": t.TaskType,
			"source_instance_id": t.SourceID, "target_instance_id": t.TargetID,
			"source_db": t.SourceDB, "target_db": t.TargetDB,
			"source_host": sourceHost, "source_port": sourcePort,
			"source_instance":    store.BriefOfInstance(inst),
			"synced_table_count": synced,
			"publication_name":   extra["publication_name"], "env": taskEnv,
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
	c.JSON(http.StatusOK, gin.H{"data": h.effectiveOccupiedSourceTables(c.Request.Context(), init)})
}

func (h *CDCHandler) SyncedTablesBatch(c *gin.Context) {
	var req struct {
		IDs []string `json:"parent_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	var inits []*store.CDCTask
	for _, id := range uniqueNonEmpty(req.IDs) {
		if t, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c)); t != nil {
			inits = append(inits, t)
		}
	}
	occ := make([][]string, len(inits))
	runBounded(len(inits), cdcBatchConcurrency, func(i int) {
		occ[i] = h.effectiveOccupiedSourceTables(c.Request.Context(), inits[i])
	})
	out := map[string][]string{}
	for i, t := range inits {
		out[t.ID] = occ[i]
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

const cdcBatchConcurrency = 8

func runBounded(n, limit int, fn func(i int)) {
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

// listSourceSchemaTables 列出父任务源库 schema 下的基表（按任务生效地址连接）。
func (h *CDCHandler) listSourceSchemaTables(ctx context.Context, inst *store.Instance, init *store.CDCTask) ([]string, error) {
	initExtra := cdc.ParseTaskExtra(init.Description)
	db, err := dbmig.OpenInstance(ctx, cdc.TaskSourceInstance(inst, initExtra), h.Secret, init.SourceDB)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	schema := initExtra["source_schema"]
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=$1 AND table_type='BASE TABLE' ORDER BY table_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil && n != "" && !strings.EqualFold(n, "debezium_signal") {
			all = append(all, n)
		}
	}
	return all, rows.Err()
}

func subtractOccupied(all, occupied []string) []string {
	synced := map[string]struct{}{}
	for _, t := range occupied {
		synced[strings.ToLower(t)] = struct{}{}
	}
	avail := []string{}
	for _, t := range all {
		if _, ok := synced[strings.ToLower(t)]; !ok {
			avail = append(avail, t)
		}
	}
	return avail
}

func (h *CDCHandler) AvailableTables(c *gin.Context) {
	init, err := h.resolveInitTask(c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	inst, _ := resolveTaskInstance(h.Store, c, init, init.SourceID)
	if inst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "源端实例不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	all, err := h.listSourceSchemaTables(ctx, inst, init)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	occ := h.effectiveOccupancy(ctx, init)
	c.JSON(http.StatusOK, gin.H{
		"data":         subtractOccupied(all, occ.Tables),
		"in_flight":    nonNilEntries(occ.InFlight),
		"stale":        nonNilEntries(occ.Stale),
		"live_checked": occ.LiveChecked,
	})
}

func nonNilEntries(in []cdcOccupancyEntry) []cdcOccupancyEntry {
	if in == nil {
		return []cdcOccupancyEntry{}
	}
	return in
}

type cdcParentOccupancyEntry struct {
	ParentID   string `json:"parent_id"`
	ParentName string `json:"parent_name"`
	SourceDB   string `json:"source_db"`
	cdcOccupancyEntry
}

// AvailableTablesBatch POST /cdc-sync/tasks/add-table/available-tables/batch —
// 逐父任务连本库列表、减本父任务占用，返回并集及每张表可加/存在的父任务数。
func (h *CDCHandler) AvailableTablesBatch(c *gin.Context) {
	var req struct {
		IDs []string `json:"parent_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	ids := uniqueNonEmpty(req.IDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "parent_ids 必填"})
		return
	}
	if len(ids) > cdcBatchAddTableMaxParents {
		c.JSON(http.StatusBadRequest, gin.H{"message": "单次最多 128 个父任务"})
		return
	}
	type failItem struct {
		ParentID string `json:"parent_id"`
		Name     string `json:"name,omitempty"`
		Message  string `json:"message"`
	}
	type parentOut struct {
		Exists []string `json:"exists"`
		// ExistsChecked=false：分库模式仅列了代表库，不能据此判定表不存在
		ExistsChecked bool     `json:"exists_checked"`
		Occupied      []string `json:"occupied"`
	}
	type job struct {
		init *store.CDCTask
		inst *store.Instance
	}
	var jobs []job
	failed := make([]failItem, 0)
	tid := effectiveListTenantID(h.Store, c)
	for _, id := range ids {
		t, _ := h.Store.GetCDCInTenant(id, tid)
		if t == nil || t.TaskType != "init" {
			failed = append(failed, failItem{ParentID: id, Message: "父任务不存在"})
			continue
		}
		inst, _ := resolveTaskInstance(h.Store, c, t, t.SourceID)
		if inst == nil {
			failed = append(failed, failItem{ParentID: id, Name: t.Name, Message: "源端实例不存在"})
			continue
		}
		jobs = append(jobs, job{init: t, inst: inst})
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	alls := make([][]string, len(jobs))
	occs := make([]cdcOccupancy, len(jobs))
	errs := make([]error, len(jobs))
	runBounded(len(jobs), cdcBatchConcurrency, func(i int) {
		alls[i], errs[i] = h.listSourceSchemaTables(ctx, jobs[i].inst, jobs[i].init)
		if errs[i] == nil {
			occs[i] = h.effectiveOccupancy(ctx, jobs[i].init)
		}
	})
	type tableAgg struct {
		name            string
		addable, exists int
	}
	agg := map[string]*tableAgg{}
	var order []string
	parents := map[string]parentOut{}
	inFlight := make([]cdcParentOccupancyEntry, 0)
	stale := make([]cdcParentOccupancyEntry, 0)
	for i, j := range jobs {
		if errs[i] != nil {
			failed = append(failed, failItem{ParentID: j.init.ID, Name: j.init.Name, Message: errs[i].Error()})
			continue
		}
		occTables := occs[i].Tables
		if occTables == nil {
			occTables = []string{}
		}
		exists := alls[i]
		if exists == nil {
			exists = []string{}
		}
		parents[j.init.ID] = parentOut{
			Exists:        exists,
			ExistsChecked: !cdc.ShardModeActive(cdc.ParseTaskExtra(j.init.Description)),
			Occupied:      occTables,
		}
		avail := map[string]struct{}{}
		for _, t := range subtractOccupied(alls[i], occTables) {
			avail[strings.ToLower(t)] = struct{}{}
		}
		for _, t := range alls[i] {
			k := strings.ToLower(t)
			a := agg[k]
			if a == nil {
				a = &tableAgg{name: t}
				agg[k] = a
				order = append(order, k)
			}
			a.exists++
			if _, ok := avail[k]; ok {
				a.addable++
			}
		}
		for _, e := range occs[i].InFlight {
			inFlight = append(inFlight, cdcParentOccupancyEntry{ParentID: j.init.ID, ParentName: j.init.Name, SourceDB: j.init.SourceDB, cdcOccupancyEntry: e})
		}
		for _, e := range occs[i].Stale {
			stale = append(stale, cdcParentOccupancyEntry{ParentID: j.init.ID, ParentName: j.init.Name, SourceDB: j.init.SourceDB, cdcOccupancyEntry: e})
		}
	}
	sort.Strings(order)
	tables := make([]gin.H, 0, len(order))
	for _, k := range order {
		a := agg[k]
		tables = append(tables, gin.H{"name": a.name, "addable": a.addable, "exists": a.exists})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"tables":    tables,
		"parents":   parents,
		"in_flight": inFlight,
		"stale":     stale,
		"failed":    failed,
		"total":     len(ids),
	}})
}

func (h *CDCHandler) Publication(c *gin.Context) {
	init, err := h.resolveInitTask(c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	extra := cdc.ParseTaskExtra(init.Description)
	pub := extra["publication_name"]
	inst, _ := resolveTaskInstance(h.Store, c, init, init.SourceID)
	live := []string{}
	msg := ""
	if inst != nil && pub != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
		defer cancel()
		if db, oerr := dbmig.OpenInstance(ctx, cdc.TaskSourceInstance(inst, extra), h.Secret, init.SourceDB); oerr == nil {
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
