package handler

import (
	"context"
	"encoding/json"
	"fmt"
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
	if extra := h.cdcInitParentsViaGrant(c); len(extra) > 0 {
		have := map[string]struct{}{}
		for i := range list {
			have[list[i].ID] = struct{}{}
		}
		for _, t := range extra {
			if _, ok := have[t.ID]; ok {
				continue
			}
			list = append(list, t)
		}
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
	// 列表/勾选用平台记录即可，不连源库核对 publication（live 核对只在可选表/提交）
	c.JSON(http.StatusOK, gin.H{"data": h.listOccupiedSourceTables(init)})
}

func (h *CDCHandler) SyncedTablesBatch(c *gin.Context) {
	var req struct {
		IDs []string `json:"parent_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	out := map[string][]string{}
	for _, id := range uniqueNonEmpty(req.IDs) {
		if t, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c)); t != nil {
			out[id] = h.listOccupiedSourceTables(t)
		}
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

// listSourceSchemaTables 列出父任务源库 schema 下的基表（单连接，顺带可复用 meta）。
func (h *CDCHandler) listSourceSchemaTables(ctx context.Context, inst *store.Instance, init *store.CDCTask) ([]string, error) {
	meta := h.loadSourceMetaLocal(ctx, inst, init)
	if meta.Error != "" {
		return nil, fmt.Errorf("%s", meta.Error)
	}
	return meta.Tables, nil
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
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	metas := h.loadSourceMetaForParents(ctx, []cdcMetaJob{{Init: init, Inst: inst}})
	meta := metas[0]
	if meta.Error != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": meta.Error})
		return
	}
	occ := h.occupancyFromMeta(init, meta)
	c.JSON(http.StatusOK, gin.H{
		"data":         subtractOccupied(meta.Tables, occ.Tables),
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

// cdcParentTableRef 批量可选表中「某父任务 / 其 SourceDB」引用，用于缺表与占用明细。
type cdcParentTableRef struct {
	ParentID string `json:"parent_id"`
	Name     string `json:"name,omitempty"`
	SourceDB string `json:"source_db,omitempty"`
}

type batchAvailParentSnap struct {
	Ref           cdcParentTableRef
	InstanceName  string
	ViaAgent      bool
	ExistsChecked bool
	OccupiedCount int
	TableSet      map[string]struct{} // lower(table) → present in SourceDB
	AvailSet      map[string]struct{} // lower(table) → present and not occupied
}

type batchAvailTableAgg struct {
	Name              string
	Addable           int
	Exists            int
	MissingParentIDs  []string
	OccupiedParentIDs []string
}

// buildBatchAvailTableAggs 两遍聚合：先收集各父 SourceDB 表集合与全局表名，再按表归类有表/可加/缺表/占用。
// 缺表/占用只记 parent_id，展示字段由响应里的 parents 字典解析，避免包体膨胀。
func buildBatchAvailTableAggs(snaps []batchAvailParentSnap) (order []string, agg map[string]*batchAvailTableAgg) {
	agg = map[string]*batchAvailTableAgg{}
	seen := map[string]struct{}{}
	for _, s := range snaps {
		for k := range s.TableSet {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			order = append(order, k)
		}
	}
	sort.Strings(order)
	for _, k := range order {
		agg[k] = &batchAvailTableAgg{Name: k, MissingParentIDs: []string{}, OccupiedParentIDs: []string{}}
	}
	for _, s := range snaps {
		pid := s.Ref.ParentID
		for _, k := range order {
			a := agg[k]
			if _, ok := s.TableSet[k]; !ok {
				a.MissingParentIDs = append(a.MissingParentIDs, pid)
				continue
			}
			a.Exists++
			if _, ok := s.AvailSet[k]; ok {
				a.Addable++
			} else {
				a.OccupiedParentIDs = append(a.OccupiedParentIDs, pid)
			}
		}
	}
	return order, agg
}

// AvailableTablesBatch POST /cdc-sync/tasks/add-table/available-tables/batch —
// 按父任务取表减占用；异地走 sql-agent；响应瘦身不回全量 exists/occupied，但回每表缺表/占用父任务明细。
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
		Name          string `json:"name,omitempty"`
		SourceDB      string `json:"source_db,omitempty"`
		InstanceName  string `json:"instance_name,omitempty"`
		OccupiedCount int    `json:"occupied_count"`
		ExistsChecked bool   `json:"exists_checked"`
		ViaAgent      bool   `json:"via_agent,omitempty"`
	}
	failed := make([]failItem, 0)
	tid := effectiveListTenantID(h.Store, c)
	var jobs []cdcMetaJob
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
		jobs = append(jobs, cdcMetaJob{Init: t, Inst: inst})
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	metas := h.loadSourceMetaForParents(ctx, jobs)

	parents := map[string]parentOut{}
	inFlight := make([]cdcParentOccupancyEntry, 0)
	stale := make([]cdcParentOccupancyEntry, 0)
	snaps := make([]batchAvailParentSnap, 0, len(jobs))
	displayName := map[string]string{} // lower -> first-seen casing

	for i, j := range jobs {
		meta := metas[i]
		if meta.Error != "" {
			failed = append(failed, failItem{ParentID: j.Init.ID, Name: j.Init.Name, Message: meta.Error})
			continue
		}
		occ := h.occupancyFromMeta(j.Init, meta)
		instName := ""
		if j.Inst != nil {
			instName = j.Inst.Name
		}
		ref := cdcParentTableRef{ParentID: j.Init.ID, Name: j.Init.Name, SourceDB: j.Init.SourceDB}
		parents[j.Init.ID] = parentOut{
			Name:          j.Init.Name,
			SourceDB:      j.Init.SourceDB,
			InstanceName:  instName,
			OccupiedCount: len(occ.Tables),
			ExistsChecked: !cdc.ShardModeActive(cdc.ParseTaskExtra(j.Init.Description)),
			ViaAgent:      meta.ViaAgent,
		}
		tableSet := map[string]struct{}{}
		for _, t := range meta.Tables {
			k := strings.ToLower(t)
			tableSet[k] = struct{}{}
			if _, ok := displayName[k]; !ok {
				displayName[k] = t
			}
		}
		avail := map[string]struct{}{}
		for _, t := range subtractOccupied(meta.Tables, occ.Tables) {
			avail[strings.ToLower(t)] = struct{}{}
		}
		snaps = append(snaps, batchAvailParentSnap{
			Ref:           ref,
			InstanceName:  instName,
			ViaAgent:      meta.ViaAgent,
			ExistsChecked: !cdc.ShardModeActive(cdc.ParseTaskExtra(j.Init.Description)),
			OccupiedCount: len(occ.Tables),
			TableSet:      tableSet,
			AvailSet:      avail,
		})
		for _, e := range occ.InFlight {
			inFlight = append(inFlight, cdcParentOccupancyEntry{ParentID: j.Init.ID, ParentName: j.Init.Name, SourceDB: j.Init.SourceDB, cdcOccupancyEntry: e})
		}
		for _, e := range occ.Stale {
			stale = append(stale, cdcParentOccupancyEntry{ParentID: j.Init.ID, ParentName: j.Init.Name, SourceDB: j.Init.SourceDB, cdcOccupancyEntry: e})
		}
	}
	order, agg := buildBatchAvailTableAggs(snaps)
	for k, n := range displayName {
		if a := agg[k]; a != nil && n != "" {
			a.Name = n
		}
	}
	tables := make([]gin.H, 0, len(order))
	for _, k := range order {
		a := agg[k]
		row := gin.H{
			"name":    a.Name,
			"addable": a.Addable,
			"exists":  a.Exists,
		}
		if len(a.MissingParentIDs) > 0 {
			row["missing_parent_ids"] = a.MissingParentIDs
		}
		if len(a.OccupiedParentIDs) > 0 {
			row["occupied_parent_ids"] = a.OccupiedParentIDs
		}
		tables = append(tables, row)
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
