package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func (h *CDCHandler) orch() *cdc.Orchestrator {
	if h == nil {
		return nil
	}
	cfg := h.Cfg.CDC
	return cdc.NewOrchestrator(h.Store, h.Secret, cfg)
}

func (h *CDCHandler) PreviewTargetSQL(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil && c.Request.ContentLength > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": "请求体无效"})
		return
	}
	t := hydrateCDCFromMap(raw)
	if id := firstNonEmpty(t.ID, jsonAnyString(raw["id"])); id != "" {
		if exist, _ := h.Store.GetCDCInTenant(id, effectiveListTenantID(h.Store, c)); exist != nil {
			t = *exist
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	items, err := cdc.PreviewTargetSQLItems(ctx, h.Store, h.Secret, &t)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"sql": cdc.JoinTargetSQL(items), "items": items}})
}

func (h *CDCHandler) Status(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	out := cdc.AggregatePipelineStatus(ctx, h.Store, h.Secret, h.orch(), t)
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *CDCHandler) Tables(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	extra := cdc.ParseTaskExtra(t.Description)
	tables := extraCSVTables(t.Description)
	maps := cdc.ParseMappingsJSON(extra["mappings"])
	includeDeleted := c.Query("include_deleted") == "1" || strings.EqualFold(c.Query("include_deleted"), "true")
	includeDrift := c.Query("include_schema_drift") == "1" || strings.EqualFold(c.Query("include_schema_drift"), "true")
	var driftBy map[string]cdc.SchemaAlertItem
	if includeDrift {
		initID := t.ID
		if init, err := h.resolveInitTask(t.ID, middleware.TenantID(c)); err == nil && init != nil {
			initID = init.ID
		}
		if cached := cdc.GetSchemaDriftCache(initID); cached != nil {
			driftBy = map[string]cdc.SchemaAlertItem{}
			for _, it := range cached.Items {
				driftBy[strings.ToLower(it.TargetTable)] = it
			}
		}
	}
	root := h.cdcRootTask(t)
	rootExtra := cdc.ParseTaskExtra(root.Description)
	prefix := firstNonEmpty(extra["topic_prefix"], rootExtra["topic_prefix"])
	schema := firstNonEmpty(extra["source_schema"], rootExtra["source_schema"], "public")
	rlByTable := map[string]cdc.PipelineRoutineLoad{}
	if t.TargetID != "" && t.TargetDB != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
		if inst, err := resolveInstanceInTenant(h.Store, c, t.TargetID); err == nil && inst != nil {
			if db, err := dbmig.OpenInstance(ctx, inst, h.Secret, t.TargetDB); err == nil {
				targets := make([]string, 0, len(maps)+len(tables))
				if len(maps) > 0 {
					for _, m := range maps {
						if m.TargetTable != "" {
							targets = append(targets, m.TargetTable)
						}
					}
				} else {
					targets = append(targets, tables...)
				}
				for _, rl := range cdc.ListRoutineLoadStates(ctx, db, t.TargetDB, targets, t.TaskType, firstNonEmpty(extra["env"], rootExtra["env"]), t.SourceDB, prefix) {
					rlByTable[strings.ToLower(rl.Table)] = rl
				}
				_ = db.Close()
			}
		}
		cancel()
	}
	rows := make([]gin.H, 0)
	if len(maps) > 0 {
		for i, m := range maps {
			deleted := strings.EqualFold(m.Status, "deleted")
			if deleted && !includeDeleted {
				continue
			}
			tgt := firstNonEmpty(m.TargetTable, firstOf(m.SourceTables))
			topics := m.KafkaTopics
			if len(topics) == 0 {
				topics = cdc.CollectMappingKafkaTopics(prefix, schema, m)
			}
			rl := rlByTable[strings.ToLower(tgt)]
			src := strings.TrimSpace(m.Source)
			if src == "" {
				if deleted {
					src = "remove_table"
				} else {
					src = "init"
				}
			}
			row := gin.H{
				"id":           fmt.Sprintf("m%d", i),
				"target_table": tgt, "source_tables": m.SourceTables,
				"is_shard_group": cdc.IsShardMapping(m), "key_columns": m.KeyColumns,
				"primary_keys": m.KeyColumns, "shard_keys": m.ShardKeys, "partition_column": m.PartitionColumn,
				"shard_pattern":   m.ShardPattern,
				"sync_table_mode": cdc.NormalizeSyncTableMode(m.SyncTableMode),
				"source_db_scope": m.SourceDbScope, "source_db_names": m.SourceDbNames,
				"status":      firstNonEmpty(m.Status, "active"),
				"source":      src,
				"topic_names": topics,
				"routine_load_name": firstNonEmpty(
					rl.Name,
					m.RoutineLoadName,
					cdc.RoutineLoadNameForTask(t.TaskType, firstNonEmpty(extra["env"], rootExtra["env"]), t.SourceDB, prefix, tgt),
				),
				"routine_load_status": firstNonEmpty(rl.State, m.RoutineLoadStatus),
			}
			if it, ok := driftBy[strings.ToLower(tgt)]; ok {
				row["schema_has_drift"] = it.HasDrift
				row["schema_missing_columns"] = it.MissingColumns
			}
			rows = append(rows, row)
		}
	} else {
		for i, name := range tables {
			topics := []string{}
			if t := cdc.BuildKafkaTopicName(prefix, schema, name); t != "" {
				topics = []string{t}
			}
			rl := rlByTable[strings.ToLower(name)]
			rows = append(rows, gin.H{
				"id":           fmt.Sprintf("m%d", i),
				"target_table": name, "source_tables": []string{name}, "status": "active",
				"source": "init", "key_columns": []string{}, "primary_keys": []string{},
				"topic_names": topics,
				"routine_load_name": firstNonEmpty(
					rl.Name,
					cdc.RoutineLoadNameForTask(t.TaskType, firstNonEmpty(extra["env"], rootExtra["env"]), t.SourceDB, prefix, name),
				),
				"routine_load_status": rl.State,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": rows, "tables": tables})
}

func firstOf(ss []string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func (h *CDCHandler) MappingDDL(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	want := strings.TrimSpace(c.Param("mappingId"))
	if want == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "mappingId 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	items, err := cdc.PreviewTargetSQLItems(ctx, h.Store, h.Secret, t)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	extra := cdc.ParseTaskExtra(t.Description)
	maps := cdc.ParseMappingsJSON(extra["mappings"])
	var target string
	if strings.HasPrefix(want, "m") {
		if i, err := strconv.Atoi(strings.TrimPrefix(want, "m")); err == nil && i >= 0 && i < len(maps) {
			target = firstNonEmpty(maps[i].TargetTable, firstOf(maps[i].SourceTables))
		}
	}
	if target == "" {
		target = want
	}
	var hit *cdc.TargetSQLItem
	for i := range items {
		if strings.EqualFold(items[i].Table, target) {
			hit = &items[i]
			break
		}
	}
	if hit == nil && len(items) == 1 {
		hit = &items[0]
	}
	if hit == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{
			"target_table":     target,
			"create_table_sql": "",
			"routine_load_sql": "",
			"message":          "未生成该映射 DDL",
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{
		"target_table":     hit.Table,
		"create_table_sql": hit.DDL,
		"routine_load_sql": hit.Load,
	}})
}

func (h *CDCHandler) Logs(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", c.DefaultQuery("current", "1")))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", c.DefaultQuery("pageSize", "50")))
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 50
	}
	level := strings.TrimSpace(c.Query("level"))
	if level == "" {
		level = strings.TrimSpace(c.Query("log_level"))
	}
	rows, total, err := h.Store.ListCDCLogs(store.CDCLogFilter{
		TaskID:               t.ID,
		LogLevel:             level,
		Step:                 strings.TrimSpace(c.Query("step")),
		Q:                    strings.TrimSpace(c.Query("q")),
		IncludeChildTaskLogs: t.TaskType == "init",
		Limit:                size,
		Offset:               (page - 1) * size,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	items := make([]cdc.PipelineLog, 0, len(rows))
	for _, it := range rows {
		items = append(items, cdc.PipelineLogFromStore(it))
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"list":       items,
			"pagination": gin.H{"total": total, "current": page, "page_size": size},
		},
		"total": total, "page": page, "page_size": size,
	})
}

func (h *CDCHandler) Connector(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	root := h.cdcRootTask(t)
	extra := cdcExtraMap(root.Description)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()
	o := h.orch()
	presence := o.TaskConnectorPresence(ctx, root)
	missing := presence.State == cdc.PresenceNotFound
	cfg := map[string]string{}
	if !missing {
		cfg = h.loadCurrentConnectorConfig(ctx, root)
	}
	state := ""
	workerURL := cdc.NormalizeConnectBaseURL(o.ResolveConnectBase(extra))
	var tasks []any
	if name := extra["connector_name"]; name != "" && !missing {
		if st, _ := o.ConnectorStatus(ctx, extra, name); st != nil {
			if cdc.ConnectorStatusNotFound(st) {
				missing = true
			}
			if s, ok := st["connector"].(map[string]any); ok {
				if v, ok := s["state"].(string); ok {
					state = v
				}
				if w, ok := s["worker_id"].(string); ok && w != "" && workerURL == "" {
					workerURL = w
				}
			}
			if state == "" {
				if v, ok := st["state"].(string); ok {
					state = v
				}
			}
			if arr, ok := st["tasks"].([]any); ok {
				tasks = arr
			}
		}
	}
	if workerURL == "" {
		if id := extra["connector_id"]; id != "" {
			if conn, err := h.Store.GetConnector(id); err == nil && conn != nil {
				workerURL = cdc.NormalizeConnectBaseURL(conn.DBZHost)
			}
		}
	}
	if tasks == nil {
		tasks = []any{}
	}
	data := gin.H{
		"task_id": root.ID,
		"name":    extra["connector_name"], "config": cdc.MaskConfigMap(cfg),
		"template": extra["debezium_template"], "debezium_mode": extra["debezium_mode"],
		"state": state, "config_key_count": len(cfg),
		"connector_id":     extra["connector_id"],
		"connect_base_url": workerURL, "worker_url": workerURL,
		"tasks":                   tasks,
		"kafka_bootstrap_servers": extra["kafka_bootstrap_servers"],
		"message_schema_mode":     extra["message_schema_mode"],
		"connector_missing":       missing,
		"can_reinit":              false,
	}
	if missing {
		data["state"] = cdc.PresenceNotFound
		data["missing_connectors"] = presence.Missing
		data["warning"] = cdcConnectorMissingWarning
		if ok, _ := cdc.ReinitEligibleStatus(root); ok {
			data["can_reinit"] = true
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

const cdcConnectorMissingWarning = "Kafka Connect 上未找到该 Connector（可能 Debezium 服务已重建），可通过“重新初始化”补齐"

// rejectIfConnectorMissing Connector 缺失时 PUT /config 会用脱敏密码新建 Connector，须走重新初始化。
func (h *CDCHandler) rejectIfConnectorMissing(c *gin.Context, ctx context.Context, root *store.CDCTask) bool {
	if p := h.orch().TaskConnectorPresence(ctx, root); p.State == cdc.PresenceNotFound {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Connector 在 Debezium Connect 上不存在（" + strings.Join(p.Missing, ",") + "），请使用「重新初始化」补齐"})
		return true
	}
	return false
}

func (h *CDCHandler) ConnectorRuntimeStatus(c *gin.Context) {
	t, err := h.Store.GetCDCInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	root := h.cdcRootTask(t)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()
	pipe := cdc.AggregatePipelineStatus(ctx, h.Store, h.Secret, h.orch(), root)
	worst := connectorWorstState(pipe)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"connectors": pipe["connectors"], "connector": pipe["connector"],
		"worst_state": worst, "state": worst, "aggregated_state": worst,
		"connector_missing": worst == cdc.PresenceNotFound,
	}})
}

func connectorWorstState(pipe map[string]any) string {
	if pipe == nil {
		return "UNKNOWN"
	}
	if conn, ok := pipe["connector"].(map[string]any); ok {
		if s, ok := conn["state"].(string); ok && s != "" {
			return s
		}
	}
	return "UNKNOWN"
}

func cdcExtraMap(desc string) map[string]string {
	const p = "-- auto-dba-cdc:"
	i := strings.Index(desc, p)
	if i < 0 {
		return map[string]string{}
	}
	var raw map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(desc[i+len(p):])), &raw) != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for k, v := range raw {
		switch x := v.(type) {
		case string:
			out[k] = x
		case bool:
			if x {
				out[k] = "true"
			}
		default:
			b, _ := json.Marshal(v)
			out[k] = string(b)
		}
	}
	return out
}

func (h *CDCHandler) TableColumns(c *gin.Context) {
	dbName := c.Param("db")
	inst, err := resolveInstanceInTenant(h.Store, c, c.Param("id"))
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	sqlDB, err := dbmig.OpenInstance(ctx, inst, h.Secret, dbName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer sqlDB.Close()
	schema := strings.TrimSpace(c.DefaultQuery("schema", "public"))
	cols, err := cdc.ListPGColumns(ctx, sqlDB, schema, c.Param("table"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"columns": cols}})
}

func (h *CDCHandler) BatchColumns(c *gin.Context) {
	inst, err := resolveInstanceInTenant(h.Store, c, c.Param("id"))
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	var req struct {
		Schema string   `json:"schema"`
		DBName string   `json:"db_name"`
		Tables []string `json:"tables"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	dbName := strings.TrimSpace(req.DBName)
	if dbName == "" {
		dbName = c.Query("db")
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	sqlDB, err := dbmig.OpenInstance(ctx, inst, h.Secret, dbName)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer sqlDB.Close()
	schema := strings.TrimSpace(req.Schema)
	if schema == "" {
		schema = "public"
	}
	tables := map[string][]cdc.ColumnInfo{}
	for _, name := range req.Tables {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cols, cerr := cdc.ListPGColumns(ctx, sqlDB, schema, name)
		if cerr != nil {
			tables[name] = []cdc.ColumnInfo{}
			continue
		}
		tables[name] = cols
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"tables": tables}})
}

func (h *CDCHandler) DiscoverTables(c *gin.Context) {
	var req struct {
		InstanceID  string   `json:"instance_id"`
		DBName      string   `json:"db_name"`
		DBNames     []string `json:"db_names"`
		Schema      string   `json:"schema"`
		Pattern     string   `json:"pattern"`
		PatternType string   `json:"pattern_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	inst, err := resolveInstanceInTenant(h.Store, c, req.InstanceID)
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	dbs := uniqueNonEmpty(append(req.DBNames, req.DBName))
	if len(dbs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "db_name 必填"})
		return
	}
	schema := strings.TrimSpace(req.Schema)
	if schema == "" {
		schema = "public"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	seen := map[string]struct{}{}
	var matched []string
	for _, dbName := range dbs {
		sqlDB, oerr := dbmig.OpenInstance(ctx, inst, h.Secret, dbName)
		if oerr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "连接 " + dbName + " 失败: " + oerr.Error()})
			return
		}
		tables, merr := cdc.MatchShardTables(ctx, sqlDB, schema, req.Pattern, req.PatternType)
		sqlDB.Close()
		if merr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": dbName + ": " + merr.Error()})
			return
		}
		for _, t := range tables {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			matched = append(matched, t)
		}
	}
	if matched == nil {
		matched = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"pattern": req.Pattern, "pattern_type": req.PatternType, "tables": matched}})
}

func (h *CDCHandler) DiscoverDatabases(c *gin.Context) {
	var req struct {
		InstanceID     string `json:"instance_id"`
		Prefix         string `json:"prefix"`
		Representative string `json:"representative_db"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	inst, err := resolveInstanceInTenant(h.Store, c, req.InstanceID)
	if err != nil || inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	db, err := dbmig.OpenInstance(ctx, inst, h.Secret, firstNonEmpty(req.Representative, "postgres"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT datname FROM pg_database WHERE datistemplate=false ORDER BY datname`)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()
	prefix := strings.TrimSpace(req.Prefix)
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) != nil || n == "" {
			continue
		}
		if prefix != "" && !cdc.MatchNumericShardName(n, prefix) {
			continue
		}
		names = append(names, n)
	}
	if names == nil {
		names = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"prefix": prefix, "databases": names}})
}

func (h *CDCHandler) SchemaAlerts(c *gin.Context) {
	h.respondSchemaAlerts(c, c.Param("id"), strings.EqualFold(c.Query("refresh"), "true") || c.Query("refresh") == "1")
}

func (h *CDCHandler) ScanSchemaAlerts(c *gin.Context) {
	h.respondSchemaAlerts(c, c.Param("id"), true)
}

func (h *CDCHandler) ListSchemaAlerts(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": cdc.ListSchemaDriftCache()})
}

func (h *CDCHandler) ScanAllSchemaAlerts(c *gin.Context) {
	list, err := h.Store.ListCDCInTenant(effectiveListTenantID(h.Store, c), "", "init")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	scanned, drifted := 0, 0
	for i := range list {
		t := &list[i]
		if t.Status != store.StatusRunning && t.Status != store.StatusPaused {
			continue
		}
		data, serr := cdc.ScanSchemaAlerts(ctx, h.Store, h.Secret, t)
		if serr != nil {
			continue
		}
		cdc.SetSchemaDriftCache(t.ID, data)
		scanned++
		if data.DriftTableCount > 0 || data.State == cdc.SchemaAlertWarn {
			drifted++
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"scanned": scanned, "drifted": drifted, "items": cdc.ListSchemaDriftCache()}})
}

func (h *CDCHandler) respondSchemaAlerts(c *gin.Context, id string, refresh bool) {
	init, err := h.resolveInitTask(id, middleware.TenantID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}
	if !refresh {
		if cached := cdc.GetSchemaDriftCache(init.ID); cached != nil {
			c.JSON(http.StatusOK, gin.H{"data": cached})
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	data, serr := cdc.ScanSchemaAlerts(ctx, h.Store, h.Secret, init)
	if serr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": serr.Error()})
		return
	}
	cdc.SetSchemaDriftCache(init.ID, data)
	c.JSON(http.StatusOK, gin.H{"data": data})
}
