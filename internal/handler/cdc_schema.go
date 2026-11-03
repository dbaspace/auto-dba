package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

const cdcBatchAddTableMaxParents = 128

func (h *CDCHandler) resolveInitTask(id, tenantID string) (*store.CDCTask, error) {
	t, err := h.Store.GetCDCInTenant(id, tenantID)
	if err != nil || t == nil {
		return nil, fmt.Errorf("任务不存在")
	}
	if t.TaskType == "init" || strings.TrimSpace(t.ParentID) == "" {
		return t, nil
	}
	parent, err := h.Store.GetCDCInTenant(t.ParentID, tenantID)
	if err != nil || parent == nil {
		return nil, fmt.Errorf("父任务不存在")
	}
	return parent, nil
}

func (h *CDCHandler) SchemaDiff(c *gin.Context) {
	init, err := h.resolveInitTask(c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	var req struct {
		TargetTable  string   `json:"target_table"`
		SourceTables []string `json:"source_tables"`
		ColumnNames  []string `json:"column_names"`
	}
	_ = c.ShouldBindJSON(&req)
	extra := cdc.ParseTaskExtra(init.Description)
	schema := extra["source_schema"]
	if schema == "" {
		schema = "public"
	}
	maps := cdc.ParseMappingsJSON(extra["mappings"])
	items := buildSchemaDiffTargets(req.TargetTable, req.SourceTables, maps, extra["tables"])
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请指定 target_table 或父任务映射"})
		return
	}
	wantCols := map[string]struct{}{}
	for _, n := range req.ColumnNames {
		if s := strings.TrimSpace(n); s != "" {
			wantCols[strings.ToLower(s)] = struct{}{}
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	srcInst, _ := resolveTaskInstance(h.Store, c, init, init.SourceID)
	if srcInst == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "源端实例不存在"})
		return
	}
	pgDB, err := dbmig.OpenInstance(ctx, cdc.TaskSourceInstance(srcInst, extra), h.Secret, init.SourceDB)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "连接源端失败: " + err.Error()})
		return
	}
	defer pgDB.Close()

	var srDB interface{ Close() error }
	var srWarn string
	var srQuery = func(target string) ([]string, error) {
		return nil, fmt.Errorf("目标端未连接")
	}
	if init.TargetID != "" && init.TargetDB != "" {
		if tgt, _ := resolveTaskInstance(h.Store, c, init, init.TargetID); tgt != nil {
			if db, oerr := dbmig.OpenInstance(ctx, tgt, h.Secret, init.TargetDB); oerr == nil {
				srDB = db
				srQuery = func(target string) ([]string, error) {
					return cdc.ListSRColumns(ctx, db, init.TargetDB, target)
				}
			} else {
				srWarn = "目标端 StarRocks 暂不可用，仅返回 PG 候选列：" + oerr.Error()
			}
		}
	} else {
		srWarn = "未配置目标端，仅返回 PG 候选列"
	}
	if srDB != nil {
		defer srDB.Close()
	}

	out := make([]cdc.SchemaDiffItem, 0, len(items))
	for _, it := range items {
		item := cdc.SchemaDiffItem{TargetTable: it.target, SourceTables: it.sources}
		if srWarn != "" {
			item.Warnings = append(item.Warnings, srWarn)
		}
		var groups [][]cdc.ColumnInfo
		for _, src := range it.sources {
			cols, cerr := cdc.ListPGColumns(ctx, pgDB, schema, src)
			if cerr != nil {
				item.Error = cerr.Error()
				break
			}
			groups = append(groups, cols)
		}
		if item.Error != "" {
			out = append(out, item)
			continue
		}
		pgCols := cdc.IntersectColumns(groups)
		if len(wantCols) > 0 {
			var filtered []cdc.ColumnInfo
			for _, col := range pgCols {
				if _, ok := wantCols[strings.ToLower(col.Name)]; ok {
					filtered = append(filtered, col)
				}
			}
			pgCols = filtered
		}
		srNames, serr := srQuery(it.target)
		if serr != nil {
			item.Warnings = append(item.Warnings, serr.Error())
			srNames = nil
		}
		item.ColumnsToAdd = cdc.DiffPGMinusSR(pgCols, srNames)
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"init_task_id": init.ID, "items": out}})
}

func (h *CDCHandler) SchemaMigrateSQL(c *gin.Context) {
	init, err := h.resolveInitTask(c.Param("id"), middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	var req struct {
		TargetTable         string `json:"target_table"`
		KafkaDefaultOffsets string `json:"kafka_default_offsets"`
		Items               []struct {
			TargetTable string                 `json:"target_table"`
			Columns     []cdc.SchemaDiffColumn `json:"columns"`
		} `json:"items"`
		Columns []cdc.SchemaDiffColumn `json:"columns"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && c.Request.ContentLength > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请求体无效"})
		return
	}
	targetDB := init.TargetDB
	extra := cdc.ParseTaskExtra(init.Description)
	mode := extra["debezium_mode"]
	brokers := extra["kafka_bootstrap_servers"]
	prefix := extra["topic_prefix"]
	schema := extra["source_schema"]
	if schema == "" {
		schema = "public"
	}
	conc := 3
	type migrateItem struct {
		TargetTable    string            `json:"target_table"`
		ExecutionOrder []string          `json:"execution_order"`
		SQLs           map[string]string `json:"sqls"`
	}
	var items []migrateItem
	var sqls []string
	buildOne := func(target string, cols []cdc.SchemaDiffColumn) {
		if strings.TrimSpace(target) == "" {
			return
		}
		topic := prefix
		if topic != "" {
			topic = prefix + "." + schema + "." + target
		}
		offset := firstNonEmpty(req.KafkaDefaultOffsets, extra["kafka_default_offsets"], "OFFSET_BEGINNING")
		plan := cdc.BuildAlterMigratePlanOpts(targetDB, target, brokers, topic, mode, nil, cols, conc, offset, extra["message_schema_mode"], extra["env"], init.SourceDB, prefix)
		items = append(items, migrateItem{
			TargetTable:    target,
			ExecutionOrder: []string{"pause", "stop", "alter", "create"},
			SQLs:           plan,
		})
		for _, k := range []string{"pause_sql", "stop_sql", "alter_sql", "create_sql"} {
			if plan[k] != "" {
				sqls = append(sqls, plan[k])
			}
		}
	}
	if len(req.Items) > 0 {
		for _, it := range req.Items {
			buildOne(it.TargetTable, it.Columns)
		}
	} else if req.TargetTable != "" {
		buildOne(req.TargetTable, req.Columns)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"init_task_id": init.ID, "sql": strings.Join(sqls, "\n"), "items": items}})
}

func (h *CDCHandler) BatchAddTablePrecheck(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := cdcParentIDsFromMap(raw)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请至少选择一个 parent_id"})
		return
	}
	if len(ids) > cdcBatchAddTableMaxParents {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("父任务过多（最多 %d）", cdcBatchAddTableMaxParents)})
		return
	}
	newTables := jsonAnyStringSlice(raw["tables"])
	if len(newTables) == 0 {
		t := hydrateCDCFromMap(raw)
		newTables = extraCSVTables(t.Description)
	}
	items := make([]gin.H, 0, len(ids))
	okN, skipN, failN := 0, 0, 0
	resolved := make([]*store.CDCTask, len(ids))
	for i, id := range ids {
		if p, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c)); p != nil && p.TaskType == "init" && p.Status == store.StatusRunning {
			resolved[i] = p
		}
	}
	states := h.parentTableStates(c.Request.Context(), resolved)
	for idx, id := range ids {
		item := gin.H{"parent_id": id}
		checks := make([]gin.H, 0, 4)
		addCheck := func(code, name, level, status, msg string) {
			checks = append(checks, gin.H{"code": code, "name": name, "level": level, "status": status, "message": msg})
		}
		p, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c))
		if p == nil || p.TaskType != "init" {
			addCheck("parent_task", "父任务", "required", "failed", "不是初始化任务")
			item["ok"] = false
			item["ready"] = false
			item["message"] = "不是初始化任务"
			item["checks"] = checks
			failN++
			items = append(items, item)
			continue
		}
		item["parent_name"] = p.Name
		item["source_db"] = p.SourceDB
		item["source_instance_id"] = p.SourceID
		if p.Status != store.StatusRunning {
			addCheck("parent_status", "父任务状态", "required", "failed", "仅 running 的初始化任务可加表，当前: "+string(p.Status))
			item["ok"] = false
			item["ready"] = false
			item["message"] = "父任务状态须为 running"
			item["checks"] = checks
			failN++
			items = append(items, item)
			continue
		}
		addCheck("parent_status", "父任务状态", "required", "passed", string(p.Status))
		pe := cdc.ParseTaskExtra(p.Description)
		if pe["publication_name"] == "" || pe["connector_name"] == "" {
			addCheck("parent_pipeline", "父任务链路", "required", "failed", "父任务尚未具备 Publication / Connector")
			item["ok"] = false
			item["ready"] = false
			item["message"] = "父任务尚未具备 Publication / Connector"
			item["checks"] = checks
			failN++
			items = append(items, item)
			continue
		}
		addCheck("parent_pipeline", "父任务链路", "required", "passed", pe["publication_name"]+" / "+pe["connector_name"])
		if inst, _ := h.Store.GetInstance(p.SourceID); inst == nil {
			addCheck("source_instance", "源端实例", "required", "failed", "源端实例不存在")
			item["ok"] = false
			item["ready"] = false
			item["message"] = "源端实例不存在"
			item["checks"] = checks
			failN++
			items = append(items, item)
			continue
		}
		addCheck("source_instance", "源端实例", "required", "passed", p.SourceID)
		kept, already, missing := states[idx].splitAddTables(newTables)
		skipped := append(append([]string{}, already...), missing...)
		if len(already) > 0 {
			addCheck("already_synced", "已同步表", "warning", "warning", "将跳过: "+cdc.JoinSample(already, 20))
		}
		if len(missing) > 0 {
			addCheck("missing_tables", "该库不存在", "warning", "warning", "将跳过: "+cdc.JoinSample(missing, 20))
		}
		item["missing_tables"] = missing
		if len(kept) == 0 {
			msg := "所选表均已在该任务同步"
			if len(missing) > 0 {
				msg = "所选表均已同步或该库不存在"
			}
			addCheck("tables_to_add", "待加表", "required", "passed", msg)
			item["ok"] = true
			item["ready"] = true
			item["skipped"] = true
			item["message"] = msg
			item["skipped_tables"] = skipped
			item["checks"] = checks
			skipN++
			items = append(items, item)
			continue
		}
		addCheck("tables_to_add", "待加表", "required", "passed", fmt.Sprintf("%d 张", len(kept)))
		item["ok"] = true
		item["ready"] = true
		item["tables"] = kept
		item["skipped_tables"] = skipped
		item["checks"] = checks
		okN++
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": failN == 0, "ready": failN == 0,
		"data": gin.H{"ready_count": okN, "skipped": skipN, "failed": failN, "items": items},
	})
}

func (h *CDCHandler) BatchAddTable(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := cdcParentIDsFromMap(raw)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请至少选择一个 parent_id"})
		return
	}
	if len(ids) > cdcBatchAddTableMaxParents {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("父任务过多（最多 %d）", cdcBatchAddTableMaxParents)})
		return
	}
	parents := make([]*store.CDCTask, 0, len(ids))
	for _, id := range ids {
		p, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c))
		if p == nil || p.TaskType != "init" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "parent " + id + " 不是初始化任务"})
			return
		}
		parents = append(parents, p)
	}
	base := hydrateCDCFromMap(raw)
	base.TaskType = "add_table"
	if base.SourceID == "" {
		base.SourceID = parents[0].SourceID
		base.SourceDB = parents[0].SourceDB
	}
	if base.Name == "" {
		base.Name = parents[0].Name + "_加表"
	}
	newTables := extraCSVTables(base.Description)
	items := make([]gin.H, 0, len(parents))
	okN, skipN, failN := 0, 0, 0
	var shared *store.Issue
	needApproval := h.cdcApprovalRequired("add_table")
	states := h.parentTableStates(c.Request.Context(), parents)
	c.Set(ctxCDCAddTablesPrefiltered, true)
	for idx, p := range parents {
		kept, _, missing := states[idx].splitAddTables(newTables)
		item := gin.H{"parent_id": p.ID, "parent_name": p.Name}
		if len(missing) > 0 {
			item["missing_tables"] = missing
		}
		if len(kept) == 0 {
			item["skipped"] = true
			item["message"] = "所选表均已在该任务同步"
			if len(missing) > 0 {
				item["message"] = "所选表均已同步或该库不存在"
			}
			skipN++
			items = append(items, item)
			continue
		}
		child := base
		child.ID = ""
		child.IssueID = ""
		child.ParentID = p.ID
		child.SourceID = p.SourceID
		child.TargetID = p.TargetID
		child.SourceDB = p.SourceDB
		child.TargetDB = p.TargetDB
		child.Description = rewriteCDCTaskTables(base.Description, kept)
		if child.Name == "" || len(parents) > 1 {
			child.Name = strings.TrimSpace(base.Name)
			if child.Name == "" {
				child.Name = p.Name + "_加表"
			} else if len(parents) > 1 {
				child.Name = child.Name + "_" + p.Name
			}
		}
		if err := h.prepareCDCCreate(c, &child); err != nil {
			item["ok"] = false
			item["message"] = err.Error()
			failN++
			items = append(items, item)
			continue
		}
		if needApproval && shared == nil {
			issue, ierr := h.createSharedCDCIssue(c, &child, cdcReviewersFromMap(raw)...)
			if ierr != nil {
				item["ok"] = false
				item["message"] = ierr.Error()
				failN++
				items = append(items, item)
				continue
			}
			shared = issue
		}
		created, err := h.persistCDCTaskShared(c, &child, shared, cdcReviewersFromMap(raw)...)
		if err != nil {
			item["ok"] = false
			item["message"] = err.Error()
			failN++
			items = append(items, item)
			continue
		}
		item["ok"] = true
		item["task"] = created
		okN++
		items = append(items, item)
	}
	issueID := ""
	if shared != nil {
		issueID = shared.ID
	}
	c.JSON(http.StatusOK, gin.H{"ok": failN == 0, "data": gin.H{"created": okN, "skipped": skipN, "failed": failN, "items": items, "issue_id": issueID}})
}

func (h *CDCHandler) BatchAlterSchemaPrecheck(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := cdcParentIDsFromMap(raw)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请至少选择一个 parent_id"})
		return
	}
	if len(ids) > cdcBatchAddTableMaxParents {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("父任务过多（最多 %d）", cdcBatchAddTableMaxParents)})
		return
	}
	base := hydrateCDCFromMap(raw)
	wanted := extraCSVTables(base.Description)
	alterItems := parseAlterSchemaTargets(base.Description)
	if len(wanted) == 0 {
		for _, it := range alterItems {
			wanted = append(wanted, it)
		}
	}
	if len(wanted) == 0 {
		wanted = jsonAnyStringSlice(raw["tables"])
	}
	items := make([]gin.H, 0, len(ids))
	okN, skipN, failN := 0, 0, 0
	for _, id := range ids {
		item := gin.H{"parent_id": id}
		checks := make([]gin.H, 0, 3)
		addCheck := func(code, name, level, status, msg string) {
			checks = append(checks, gin.H{"code": code, "name": name, "level": level, "status": status, "message": msg})
		}
		p, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c))
		if p == nil || p.TaskType != "init" {
			addCheck("parent_task", "父任务", "required", "failed", "不是初始化任务")
			item["ok"] = false
			item["ready"] = false
			item["message"] = "不是初始化任务"
			item["checks"] = checks
			failN++
			items = append(items, item)
			continue
		}
		item["parent_name"] = p.Name
		item["source_db"] = p.SourceDB
		synced := extraCSVTables(p.Description)
		kept := intersectFold(wanted, synced)
		if len(kept) == 0 {
			addCheck("target_tables", "目标表", "required", "passed", "该任务没有匹配的目标表")
			item["ok"] = true
			item["ready"] = true
			item["skipped"] = true
			item["message"] = "该任务没有匹配的目标表"
			item["checks"] = checks
			skipN++
			items = append(items, item)
			continue
		}
		addCheck("target_tables", "目标表", "required", "passed", fmt.Sprintf("%d 张", len(kept)))
		item["ok"] = true
		item["ready"] = true
		item["tables"] = kept
		item["checks"] = checks
		okN++
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": failN == 0, "ready": failN == 0,
		"data": gin.H{"ready_count": okN, "skipped": skipN, "failed": failN, "items": items},
	})
}

func (h *CDCHandler) BatchAlterSchema(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ids := cdcParentIDsFromMap(raw)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请至少选择一个 parent_id"})
		return
	}
	if len(ids) > cdcBatchAddTableMaxParents {
		c.JSON(http.StatusBadRequest, gin.H{"message": fmt.Sprintf("父任务过多（最多 %d）", cdcBatchAddTableMaxParents)})
		return
	}
	parents := make([]*store.CDCTask, 0, len(ids))
	for _, id := range ids {
		p, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c))
		if p == nil || p.TaskType != "init" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "parent " + id + " 不是初始化任务"})
			return
		}
		parents = append(parents, p)
	}
	reviewers := cdcReviewersFromMap(raw)
	base := hydrateCDCFromMap(raw)
	base.TaskType = "alter_schema"
	if base.SourceID == "" {
		base.SourceID = parents[0].SourceID
		base.SourceDB = parents[0].SourceDB
	}
	if base.Name == "" {
		base.Name = parents[0].Name + "_加列"
	}
	wanted := extraCSVTables(base.Description)
	alterItems := parseAlterSchemaTargets(base.Description)
	if len(wanted) == 0 {
		for _, it := range alterItems {
			wanted = append(wanted, it)
		}
	}
	items := make([]gin.H, 0, len(parents))
	okN, skipN, failN := 0, 0, 0
	var shared *store.Issue
	needApproval := h.cdcApprovalRequired("alter_schema")
	for _, p := range parents {
		synced := h.listOccupiedSourceTables(p)
		kept := intersectFold(wanted, synced)
		item := gin.H{"parent_id": p.ID, "parent_name": p.Name}
		if len(kept) == 0 {
			item["skipped"] = true
			item["message"] = "该任务没有匹配的目标表"
			skipN++
			items = append(items, item)
			continue
		}
		child := base
		child.ID = ""
		child.IssueID = ""
		child.ParentID = p.ID
		child.SourceID = p.SourceID
		child.TargetID = p.TargetID
		child.SourceDB = p.SourceDB
		child.TargetDB = p.TargetDB
		if child.Name == "" || len(parents) > 1 {
			child.Name = strings.TrimSpace(base.Name)
			if child.Name == "" {
				child.Name = p.Name + "_加列"
			} else if len(parents) > 1 {
				child.Name = child.Name + "_" + p.Name
			}
		}
		if err := h.prepareCDCCreate(c, &child); err != nil {
			item["ok"] = false
			item["message"] = err.Error()
			failN++
			items = append(items, item)
			continue
		}
		if needApproval && shared == nil {
			issue, ierr := h.createSharedCDCIssue(c, &child, reviewers...)
			if ierr != nil {
				item["ok"] = false
				item["message"] = ierr.Error()
				failN++
				items = append(items, item)
				continue
			}
			shared = issue
		}
		created, err := h.persistCDCTaskShared(c, &child, shared, reviewers...)
		if err != nil {
			item["ok"] = false
			item["message"] = err.Error()
			failN++
			items = append(items, item)
			continue
		}
		item["ok"] = true
		item["task"] = created
		okN++
		items = append(items, item)
	}
	issueID := ""
	if shared != nil {
		issueID = shared.ID
	}
	c.JSON(http.StatusOK, gin.H{"ok": failN == 0, "data": gin.H{"created": okN, "skipped": skipN, "failed": failN, "items": items, "issue_id": issueID}})
}

func parseAlterSchemaTargets(desc string) []string {
	extra := cdc.ParseTaskExtra(desc)
	raw := strings.TrimSpace(extra["alter_schema_tables"])
	if raw == "" {
		return nil
	}
	var items []struct {
		TargetTable string `json:"target_table"`
	}
	if json.Unmarshal([]byte(raw), &items) != nil {
		return nil
	}
	var out []string
	for _, it := range items {
		if s := strings.TrimSpace(it.TargetTable); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func intersectFold(a, b []string) []string {
	m := map[string]struct{}{}
	for _, x := range b {
		m[strings.ToLower(x)] = struct{}{}
	}
	var out []string
	for _, x := range a {
		if _, ok := m[strings.ToLower(x)]; ok {
			out = append(out, x)
		}
	}
	return out
}

type schemaDiffTarget struct {
	target  string
	sources []string
}

func buildSchemaDiffTargets(target string, sources []string, maps []cdc.TableMapping, tablesCSV string) []schemaDiffTarget {
	target = strings.TrimSpace(target)
	if target != "" {
		src := sources
		if len(src) == 0 {
			for _, m := range maps {
				if strings.EqualFold(m.TargetTable, target) {
					src = m.SourceTables
					break
				}
			}
		}
		if len(src) == 0 {
			src = []string{target}
		}
		return []schemaDiffTarget{{target: target, sources: src}}
	}
	var out []schemaDiffTarget
	for _, m := range maps {
		if strings.TrimSpace(m.TargetTable) == "" {
			continue
		}
		src := m.SourceTables
		if len(src) == 0 {
			src = []string{m.TargetTable}
		}
		out = append(out, schemaDiffTarget{target: m.TargetTable, sources: src})
	}
	if len(out) > 0 {
		return out
	}
	for _, t := range strings.Split(tablesCSV, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, schemaDiffTarget{target: t, sources: []string{t}})
		}
	}
	return out
}

func extraCSVTables(desc string) []string {
	extra := cdc.ParseTaskExtra(desc)
	var out []string
	for _, p := range strings.FieldsFunc(extra["tables"], func(r rune) bool { return r == ',' || r == '，' }) {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func subtractFold(all, drop []string) []string {
	m := map[string]struct{}{}
	for _, d := range drop {
		m[strings.ToLower(d)] = struct{}{}
	}
	var out []string
	for _, a := range all {
		if _, ok := m[strings.ToLower(a)]; ok {
			continue
		}
		out = append(out, a)
	}
	return out
}

func uniqueNonEmpty(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
