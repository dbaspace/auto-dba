package handler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/cdc"
	"auto-dba/internal/dbmig"
)

type cdcPrecheckItem struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Level   string `json:"level"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type cdcPrecheckNaming struct {
	PublicationName       string `json:"publication_name,omitempty"`
	SlotName              string `json:"slot_name,omitempty"`
	ConnectorName         string `json:"connector_name,omitempty"`
	TopicPrefix           string `json:"topic_prefix,omitempty"`
	KafkaBootstrapServers string `json:"kafka_bootstrap_servers,omitempty"`
	ConnectBaseURL        string `json:"connect_base_url,omitempty"`
}

var cdcNamingRE = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)

// 预检附带同步生成 sql_previews 的表/映射数量上限；超过则跳过，改走独立 preview 接口。
const cdcSQLPreviewSkipThreshold = 100

func cdcShouldSkipSQLPreviews(mappingCount, tableCount int) bool {
	n := mappingCount
	if n == 0 {
		n = tableCount
	}
	return n >= cdcSQLPreviewSkipThreshold
}

func (h *CDCHandler) Precheck(c *gin.Context) {
	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "ready": false, "message": "请求体无效"})
		return
	}
	extra := parseCDCExtraMap(jsonAnyString(raw["description"]))
	if extra == nil {
		extra = map[string]any{}
	}
	mergeFlatCDCFields(raw, extra)
	pick := func(keys ...string) string {
		for _, k := range keys {
			if s := jsonAnyString(raw[k]); s != "" {
				return s
			}
			if extra != nil {
				if s := jsonAnyString(extra[k]); s != "" {
					return s
				}
			}
		}
		return ""
	}
	srcID, dstID := cdcInstanceIDsFromPayload(raw)
	taskType := pick("task_type")
	if taskType == "" {
		taskType = "init"
	}
	autoCreate := cdcAutoCreateEnabled(raw)
	var checks []cdcPrecheckItem
	add := func(it cdcPrecheckItem) { checks = append(checks, it) }

	if srcID == "" {
		add(cdcPrecheckItem{Code: "request", Name: "请求", Level: "required", Status: "failed", Message: "source_instance_id 必填"})
		c.JSON(http.StatusOK, gin.H{"ok": false, "ready": false, "message": "source_instance_id 必填", "data": gin.H{"ready": false, "checks": checks}})
		return
	}
	add(cdcPrecheckItem{Code: "request", Name: "请求", Level: "required", Status: "passed", Message: "任务类型 " + taskType})

	src, _ := h.Store.GetInstance(srcID)
	if src == nil {
		add(cdcPrecheckItem{Code: "source_instance", Name: "源端实例", Level: "required", Status: "failed", Message: "源端实例未登记"})
	} else {
		add(cdcPrecheckItem{Code: "source_instance", Name: "源端实例", Level: "required", Status: "passed", Message: src.Name})
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	sourceDB := pick("source_db")
	if taskType == "init" {
		pe := cdc.ParseTaskExtra(jsonAnyString(raw["description"]))
		if extra != nil {
			if pe["source_db_names"] == "" {
				if names, ok := extra["source_db_names"].([]any); ok {
					parts := make([]string, 0, len(names))
					for _, n := range names {
						if s, ok := n.(string); ok && strings.TrimSpace(s) != "" {
							parts = append(parts, s)
						}
					}
					pe["source_db_names"] = strings.Join(parts, ",")
				}
			}
			if jsonAnyString(extra["source_scope_mode"]) == "multi_db" || jsonAnyBool(extra["shard_db"]) {
				pe["source_scope_mode"] = "multi_db"
				pe["shard_db"] = "true"
			}
			if pe["source_db_prefix"] == "" {
				pe["source_db_prefix"] = jsonAnyString(extra["source_db_prefix"])
			}
		}
		if err := cdc.ValidateShardCreate(pe, sourceDB); err != nil {
			add(cdcPrecheckItem{Code: "shard_db", Name: "多分库范围", Level: "required", Status: "failed", Message: err.Error()})
		} else if cdc.ShardModeActive(pe) {
			shardDBs := cdc.ResolveShardDBs(pe, sourceDB)
			add(cdcPrecheckItem{
				Code: "shard_db", Name: "多分库范围", Level: "required", Status: "passed",
				Message: cdc.FormatShardPrecheckRepOnlyMessage(shardDBs, sourceDB),
			})
			add(cdcPrecheckItem{
				Code: "shard_source_tables", Name: "分库源表", Level: "warning", Status: "warning",
				Message: "预检不连非代表库扫表；创建后编排仍按各分库执行，若某分库缺表将在运行期暴露",
			})
		}
	}
	var checkedTables []string
	if src != nil {
		if db, err := dbmig.OpenInstance(ctx, src, h.Secret, sourceDB); err != nil {
			add(cdcPrecheckItem{Code: "source_connectivity", Name: "源端连通", Level: "required", Status: "failed", Message: err.Error()})
		} else {
			add(cdcPrecheckItem{Code: "source_connectivity", Name: "源端连通", Level: "required", Status: "passed", Message: "ok"})
			if lvl, werr := cdc.PGWalLevel(ctx, db); werr != nil {
				add(cdcPrecheckItem{Code: "wal_level", Name: "wal_level", Level: "required", Status: "warning", Message: werr.Error()})
			} else if !strings.EqualFold(lvl, "logical") {
				add(cdcPrecheckItem{Code: "wal_level", Name: "wal_level", Level: "required", Status: "failed", Message: "当前 " + lvl + "，须为 logical"})
			} else {
				add(cdcPrecheckItem{Code: "wal_level", Name: "wal_level", Level: "required", Status: "passed", Message: lvl})
			}
			schema := pick("source_schema")
			if schema == "" {
				schema = "public"
			}
			var tables []string
			if extra != nil {
				if arr, ok := extra["tables"].([]any); ok {
					for _, n := range arr {
						if s, ok := n.(string); ok && strings.TrimSpace(s) != "" {
							tables = append(tables, s)
						}
					}
				}
			}
			if rawT := pick("tables"); rawT != "" && len(tables) == 0 {
				for _, p := range strings.FieldsFunc(rawT, func(r rune) bool { return r == ',' || r == '，' }) {
					if s := strings.TrimSpace(p); s != "" {
						tables = append(tables, s)
					}
				}
			}
			checkedTables = tables
			if len(tables) > 0 {
				if miss := cdc.MissingPGTables(ctx, db, schema, tables); len(miss) > 0 {
					add(cdcPrecheckItem{Code: "source_tables", Name: "源表存在", Level: "required", Status: "failed", Message: "不存在: " + cdc.JoinSample(miss, 20)})
				} else {
					add(cdcPrecheckItem{Code: "source_tables", Name: "源表存在", Level: "required", Status: "passed", Message: fmt.Sprintf("%d 张", len(tables))})
				}
			}
			db.Close()
		}
	}

	if taskType == "init" && dstID == "" && autoCreate {
		add(cdcPrecheckItem{Code: "target_instance", Name: "目标端实例", Level: "required", Status: "failed", Message: "自动建表开启时必须选择目标端 StarRocks 实例"})
	} else if dstID == "" {
		add(cdcPrecheckItem{Code: "target_instance", Name: "目标端实例", Level: "info", Status: "passed", Message: "未填写（手动 DDL）"})
	} else if dst, _ := h.Store.GetInstance(dstID); dst == nil {
		add(cdcPrecheckItem{Code: "target_instance", Name: "目标端实例", Level: "required", Status: "failed", Message: "目标端实例未登记"})
	} else {
		add(cdcPrecheckItem{Code: "target_instance", Name: "目标端实例", Level: "required", Status: "passed", Message: dst.Name})
		if db, err := dbmig.OpenInstance(ctx, dst, h.Secret, pick("target_db")); err != nil {
			add(cdcPrecheckItem{Code: "target_connectivity", Name: "目标端连通", Level: "required", Status: "failed", Message: err.Error()})
		} else {
			db.Close()
			add(cdcPrecheckItem{Code: "target_connectivity", Name: "目标端连通", Level: "required", Status: "passed", Message: "ok"})
		}
	}

	connID := pick("connector_id")
	connHost, dbzHost, schemaMode := "", "", ""
	if connID == "" {
		add(cdcPrecheckItem{Code: "connector", Name: "Debezium 集群", Level: "required", Status: "failed", Message: "未选择 Debezium 集群"})
	} else if conn, _ := h.Store.GetConnector(connID); conn == nil {
		add(cdcPrecheckItem{Code: "connector", Name: "Debezium 集群", Level: "required", Status: "failed", Message: "连接器未登记"})
	} else {
		connHost, dbzHost, schemaMode = conn.KafkaHost, conn.DBZHost, conn.MessageSchemaMode
		add(cdcPrecheckItem{Code: "connector", Name: "Debezium 集群", Level: "required", Status: "passed", Message: conn.DBZHost})
		want := pick("message_schema_mode")
		if want != "" && schemaMode != "" && want != schemaMode {
			add(cdcPrecheckItem{Code: "connector_schema_mode", Name: "消息格式", Level: "warning", Status: "warning", Message: "任务 " + want + " 与集群 " + schemaMode + " 不一致"})
		} else {
			add(cdcPrecheckItem{Code: "connector_schema_mode", Name: "消息格式", Level: "info", Status: "passed", Message: schemaMode})
		}
	}

	brokers := cdc.ResolveKafkaBootstrap(pick("kafka_bootstrap_servers"), connHost, "")
	if brokers == "" {
		add(cdcPrecheckItem{Code: "kafka_brokers", Name: "Kafka 地址", Level: "required", Status: "failed", Message: "kafka_bootstrap_servers empty: 请在连接器填写 kafka_host 或高级配置覆盖"})
	} else {
		add(cdcPrecheckItem{Code: "kafka_brokers", Name: "Kafka 地址", Level: "required", Status: "passed", Message: brokers})
	}

	tables := jsonAnyStringSlice(raw["tables"])
	if len(tables) == 0 && extra != nil {
		tables = jsonAnyStringSlice(extra["tables"])
	}
	if len(tables) == 0 && len(checkedTables) > 0 {
		tables = checkedTables
	}
	if len(tables) == 0 {
		add(cdcPrecheckItem{Code: "source_tables", Name: "同步表", Level: "required", Status: "failed", Message: "未选择同步表"})
	} else {
		add(cdcPrecheckItem{Code: "source_tables", Name: "同步表", Level: "required", Status: "passed", Message: fmt.Sprintf("%d 张: %s", len(tables), cdc.JoinSample(tables, 20))})
	}
	maps := cdcMappingsFromAny(raw["mappings"])
	if len(maps) == 0 && extra != nil {
		maps = cdcMappingsFromAny(extra["mappings"])
	}
	if initMaps := cdc.FilterInitializationMappings(maps); taskType == "add_table" && len(initMaps) > 0 {
		add(cdcPrecheckItem{Code: "debezium_signal", Name: "初始化信号表", Level: "required", Status: "passed", Message: fmt.Sprintf("%d 组 initialization 映射将写入 execute-snapshot", len(initMaps))})
	}

	dpOn := jsonAnyBool(raw["sr_dynamic_partition_enable"])
	if extra != nil && raw["sr_dynamic_partition_enable"] == nil {
		dpOn = jsonAnyBool(extra["sr_dynamic_partition_enable"])
	}
	taskPart := pick("sr_partition_column")
	if !dpOn {
		add(cdcPrecheckItem{Code: "sr_dynamic_partition_config", Name: "StarRocks 动态分区", Level: "warning", Status: "passed", Message: "未开启 sr_dynamic_partition_enable，分区键/参数不会生效"})
	} else {
		if len(maps) == 0 {
			maps = cdcMappingsFromAny(raw["mappings"])
			if len(maps) == 0 && extra != nil {
				maps = cdcMappingsFromAny(extra["mappings"])
			}
		}
		missing := []string{}
		for _, m := range maps {
			if cdc.EffectivePartitionColumn(taskPart, m.PartitionColumn) == "" {
				tgt := m.TargetTable
				if tgt == "" && len(m.SourceTables) > 0 {
					tgt = m.SourceTables[0]
				}
				missing = append(missing, tgt)
			}
		}
		if autoCreate && (len(missing) > 0 || (len(maps) == 0 && taskPart == "")) {
			msg := "开启动态分区且自动建表时须填写任务默认分区键或表级分区列"
			if len(missing) > 0 {
				msg += "：" + cdc.JoinSample(missing, 20)
			}
			add(cdcPrecheckItem{Code: "sr_dynamic_partition_config", Name: "StarRocks 动态分区", Level: "required", Status: "failed", Message: msg})
		} else {
			add(cdcPrecheckItem{Code: "sr_dynamic_partition_config", Name: "StarRocks 动态分区", Level: "required", Status: "passed", Message: "effective_partition_column=" + taskPart})
		}
	}

	resolved := cdc.ResolveInitNaming(cdc.InitNamingInput{
		Env:             pick("env"),
		SourceDB:        sourceDB,
		TaskName:        pick("name"),
		PublicationName: pick("publication_name"),
		SlotName:        pick("slot_name"),
		ConnectorName:   pick("connector_name"),
		TopicPrefix:     pick("topic_prefix"),
	})
	naming := cdcPrecheckNaming{
		PublicationName:       resolved.PublicationName,
		SlotName:              resolved.SlotName,
		ConnectorName:         resolved.ConnectorName,
		TopicPrefix:           resolved.TopicPrefix,
		KafkaBootstrapServers: brokers,
		ConnectBaseURL:        cdc.NormalizeConnectBaseURL(dbzHost),
	}
	if err := validateCdcNaming(naming); err != nil {
		add(cdcPrecheckItem{Code: "naming", Name: "命名", Level: "required", Status: "failed", Message: err.Error()})
	} else {
		add(cdcPrecheckItem{Code: "naming", Name: "命名", Level: "required", Status: "passed", Message: naming.PublicationName})
	}

	if taskType == "add_table" {
		parentID := firstNonEmpty(pick("parent_id"), pick("parent_task_id"))
		if parentID == "" {
			ids := jsonAnyStringSlice(raw["parent_ids"])
			if len(ids) > 0 {
				parentID = ids[0]
			}
		}
		if parentID == "" {
			add(cdcPrecheckItem{Code: "parent_task", Name: "父任务", Level: "required", Status: "failed", Message: "add_table 须指定 parent_id"})
		} else if parent, _ := h.Store.GetCDC(parentID); parent == nil {
			add(cdcPrecheckItem{Code: "parent_task", Name: "父任务", Level: "required", Status: "failed", Message: "父任务不存在"})
		} else {
			pe := cdc.ParseTaskExtra(parent.Description)
			if pe["publication_name"] == "" || pe["connector_name"] == "" {
				add(cdcPrecheckItem{Code: "parent_pipeline", Name: "父任务链路", Level: "required", Status: "failed", Message: "父任务尚未具备 Publication / Connector"})
			} else {
				add(cdcPrecheckItem{Code: "parent_pipeline", Name: "父任务链路", Level: "required", Status: "passed", Message: pe["publication_name"] + " / " + pe["connector_name"]})
			}
			synced := extraCSVTables(parent.Description)
			skip := subtractFold(tables, synced)
			if len(synced) > 0 && len(skip) < len(tables) {
				already := subtractFold(tables, skip)
				add(cdcPrecheckItem{Code: "already_synced", Name: "已同步表", Level: "warning", Status: "warning", Message: "将跳过: " + cdc.JoinSample(already, 20)})
			}
			if pe["auto_create_table"] == "false" && autoCreate {
				add(cdcPrecheckItem{Code: "parent_auto_create", Name: "自动建表", Level: "warning", Status: "warning", Message: "父任务为手动 DDL，加表自动建表可能无法执行 Routine Load"})
			}
		}
	}

	if naming.ConnectorName != "" || naming.SlotName != "" {
		if list, lerr := h.Store.ListCDC("", "init"); lerr == nil {
			var hits []string
			for i := range list {
				it := &list[i]
				if it.Status != "running" && it.Status != "paused" && it.Status != "init" {
					continue
				}
				if parentSkip := firstNonEmpty(pick("parent_id"), pick("parent_task_id")); parentSkip != "" && it.ID == parentSkip {
					continue
				}
				pe := cdc.ParseTaskExtra(it.Description)
				if naming.ConnectorName != "" && strings.EqualFold(pe["connector_name"], naming.ConnectorName) {
					hits = append(hits, it.Name+" connector")
				} else if naming.SlotName != "" && strings.EqualFold(pe["slot_name"], naming.SlotName) && it.SourceID == srcID {
					hits = append(hits, it.Name+" slot")
				}
			}
			if len(hits) > 0 {
				add(cdcPrecheckItem{Code: "hub_task_conflict", Name: "命名冲突", Level: "required", Status: "failed", Message: "与运行中任务冲突: " + cdc.JoinSample(hits, 20)})
			} else {
				add(cdcPrecheckItem{Code: "hub_task_conflict", Name: "命名冲突", Level: "info", Status: "passed", Message: "无冲突"})
			}
		}
	}

	ready := true
	failed := []string{}
	for _, it := range checks {
		if it.Level == "required" && it.Status == "failed" {
			ready = false
			failed = append(failed, it.Name)
		}
	}
	msg := "预检通过"
	if !ready {
		msg = "预检未通过：" + strings.Join(failed, "、")
	}
	data := gin.H{"ready": ready, "naming": naming, "checks": checks}
	previewN := len(maps)
	if previewN == 0 {
		previewN = len(tables)
	}
	if ready && autoCreate && dstID != "" && srcID != "" {
		if cdcShouldSkipSQLPreviews(len(maps), len(tables)) {
			add(cdcPrecheckItem{
				Code: "target_sql_preview_skipped", Name: "目标 SQL 预览", Level: "warning", Status: "warning",
				Message: fmt.Sprintf("表/映射数 %d ≥ %d，预检跳过同步生成 sql_previews，请在下一步使用独立预览接口", previewN, cdcSQLPreviewSkipThreshold),
			})
			data["checks"] = checks
		} else {
			previewTask := hydrateCDCFromMap(raw)
			pctx, pcancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
			if items, perr := cdc.PreviewTargetSQLItems(pctx, h.Store, h.Secret, &previewTask); perr == nil {
				data["sql_previews"] = items
			}
			pcancel()
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": ready, "ready": ready, "message": msg,
		"data": data,
	})
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cdcSanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	b := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b = append(b, r)
		default:
			b = append(b, '_')
		}
	}
	return strings.Trim(strings.ToLower(string(b)), "_")
}

func validateCdcNaming(n cdcPrecheckNaming) error {
	for _, p := range []struct{ label, v string }{
		{"Publication", n.PublicationName},
		{"Slot", n.SlotName},
		{"Connector", n.ConnectorName},
		{"Topic 前缀", n.TopicPrefix},
	} {
		if p.v == "" {
			return nil
		}
		if !cdcNamingRE.MatchString(p.v) {
			return errBadNaming(p.label + " 仅支持字母、数字、下划线、连字符")
		}
		if p.label == "Topic 前缀" && strings.Contains(p.v, ".") {
			return errBadNaming("Topic 前缀不能包含英文句点")
		}
	}
	return nil
}

type namingErr string

func (e namingErr) Error() string { return string(e) }

func errBadNaming(s string) error { return namingErr(s) }
