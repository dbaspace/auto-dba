package cdc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

type alterSchemaItem struct {
	TargetTable  string             `json:"target_table"`
	SourceTables []string           `json:"source_tables"`
	Columns      []SchemaDiffColumn `json:"columns"`
}

func (o *Orchestrator) parentTask(t *store.CDCTask) (*store.CDCTask, error) {
	if t == nil || strings.TrimSpace(t.ParentID) == "" {
		return nil, fmt.Errorf("缺少 parent_id")
	}
	parent, err := o.Store.GetCDC(t.ParentID)
	if err != nil || parent == nil {
		return nil, fmt.Errorf("父任务不存在")
	}
	return parent, nil
}

func (o *Orchestrator) applyAddTable(ctx context.Context, t *store.CDCTask, fromStep string) error {
	parent, err := o.parentTask(t)
	if err != nil {
		return err
	}
	if !cdcTaskApproved(t) {
		return fmt.Errorf("CDC 任务须审批通过后才能执行")
	}
	extra := parseTaskExtra(t.Description)
	parentExtra := parseTaskExtra(parent.Description)
	inheritParentCDCNames(extra, parentExtra)
	tables := splitCSV(extra["tables"])
	if len(tables) == 0 {
		return fmt.Errorf("未指定新增表")
	}
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	o.appendLog(t, "INFO", "orchestrate", "add_table 开始")

	fromStep = strings.ToLower(strings.TrimSpace(fromStep))
	if extra["error_step"] != "" && fromStep == "" {
		fromStep = extra["error_step"]
	}
	runPG := fromStep == "" || fromStep == "pg"
	runDz := fromStep == "" || fromStep == "pg" || fromStep == "debezium" || fromStep == "kafka"
	runSR := fromStep == "" || fromStep == "pg" || fromStep == "debezium" || fromStep == "kafka" || fromStep == "starrocks"

	if runPG {
		if err := o.stepAddTablePG(ctx, t, parent, extra, parentExtra, tables); err != nil {
			extra["error_step"] = "pg"
			o.appendLog(t, "ERROR", "pg", err.Error())
			o.saveExtra(t, extra)
			_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
			return err
		}
		o.appendLog(t, "INFO", "pg", "ALTER PUBLICATION ADD 完成")
	}
	if fromStep == "" || fromStep == "pg" || fromStep == "kafka" {
		merged := map[string]string{}
		for k, v := range parentExtra {
			merged[k] = v
		}
		for k, v := range extra {
			if strings.TrimSpace(v) != "" {
				merged[k] = v
			}
		}
		if err := o.stepKafka(ctx, t, merged); err != nil {
			extra["error_step"] = "kafka"
			o.appendLog(t, "ERROR", "kafka", err.Error())
			o.saveExtra(t, extra)
			_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
			return err
		}
	}
	if runDz {
		if err := o.stepAddTableDebezium(ctx, t, parent, extra, parentExtra, tables); err != nil {
			extra["error_step"] = "debezium"
			o.appendLog(t, "ERROR", "debezium", err.Error())
			o.saveExtra(t, extra)
			_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
			return err
		}
		o.appendLog(t, "INFO", "debezium", "connector include.list 已合并")
	}
	if runSR && extra["auto_create_table"] != "false" && strings.TrimSpace(t.TargetID) != "" {
		sqlText, err := PreviewTargetSQL(ctx, o.Store, o.Secret, t)
		if err != nil {
			o.appendLog(t, "WARN", "starrocks", "预览目标 SQL 失败: "+err.Error())
		} else if o.Cfg.Enabled {
			if err := o.applyTargetSQL(ctx, t, sqlText); err != nil {
				extra["error_step"] = "starrocks"
				o.appendLog(t, "ERROR", "starrocks", err.Error())
				o.saveExtra(t, extra)
				_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
				return err
			}
		}
	}
	if fromStep == "" || fromStep == "pg" || fromStep == "kafka" || fromStep == "debezium" || fromStep == "starrocks" || fromStep == "snapshot" {
		if err := o.stepExecuteSnapshot(ctx, t, parent, extra, parentExtra); err != nil {
			extra["error_step"] = "snapshot"
			o.appendLog(t, "ERROR", "debezium", err.Error())
			o.saveExtra(t, extra)
			_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
			return err
		}
	}
	mergeTablesIntoParent(parent, parentExtra, tables, extra["mappings"])
	o.saveExtra(parent, parentExtra)
	delete(extra, "error_step")
	extra["pipeline_status"] = "running"
	o.saveExtra(t, extra)
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	return nil
}

func (o *Orchestrator) stepAddTablePG(ctx context.Context, t, parent *store.CDCTask, extra, parentExtra map[string]string, tables []string) error {
	inst, err := o.Store.GetInstance(parent.SourceID)
	if err != nil || inst == nil {
		return fmt.Errorf("源端实例不存在")
	}
	schema := defaultStr(extra["source_schema"], defaultStr(parentExtra["source_schema"], "public"))
	pub := defaultStr(extra["publication_name"], parentExtra["publication_name"])
	slot := defaultStr(extra["slot_name"], parentExtra["slot_name"])
	connector := defaultStr(extra["connector_name"], parentExtra["connector_name"])
	bindings := ResolveOrBuildShardBindings(parentExtra, parent.SourceDB, pub, slot, connector)
	if len(bindings) == 0 {
		bindings = []ShardDbBinding{{DbName: parent.SourceDB, PublicationName: pub, SlotName: slot, ConnectorName: connector}}
	}
	maps := ParseMappingsJSON(extra["mappings"])
	if err := ValidateMappingShardScopes(maps, ResolveShardDBs(parentExtra, parent.SourceDB)); err != nil {
		return err
	}
	signalTable := ResolveSignalTable(parentExtra)
	return runShardStepsSerial(len(bindings), func(i int) error {
		b := bindings[i]
		dbTables := TablesForShardDB(maps, tables, b.DbName)
		if len(dbTables) == 0 {
			return nil
		}
		if err := o.ensurePublicationAndSlot(ctx, inst, b.DbName, schema, b.PublicationName, b.SlotName, dbTables, true); err != nil {
			return fmt.Errorf("分库 %s: %w", b.DbName, err)
		}
		if err := o.ensureSignalOnDB(ctx, inst, b.DbName, b.PublicationName, signalTable); err != nil {
			return fmt.Errorf("分库 %s signal: %w", b.DbName, err)
		}
		o.appendLog(t, "INFO", "pg", "ADD TABLE db="+b.DbName+" pub="+b.PublicationName)
		return nil
	})
}

func (o *Orchestrator) stepExecuteSnapshot(ctx context.Context, t, parent *store.CDCTask, extra, parentExtra map[string]string) error {
	maps := FilterInitializationMappings(ParseMappingsJSON(extra["mappings"]))
	if len(maps) == 0 {
		o.appendLog(t, "INFO", "debezium", "无 initialization 映射，跳过 execute-snapshot")
		return nil
	}
	signalID := strings.TrimSpace(t.ID)
	if extra["snapshot_signal_id"] == signalID {
		o.appendLog(t, "INFO", "debezium", "snapshot signal already sent (id="+signalID+")")
		return nil
	}
	schema := defaultStr(extra["source_schema"], defaultStr(parentExtra["source_schema"], "public"))
	signalTable := ResolveSignalTable(parentExtra)
	inst, err := o.Store.GetInstance(parent.SourceID)
	if err != nil || inst == nil {
		return fmt.Errorf("源端实例不存在")
	}
	pub := defaultStr(extra["publication_name"], parentExtra["publication_name"])
	slot := defaultStr(extra["slot_name"], parentExtra["slot_name"])
	connector := defaultStr(extra["connector_name"], parentExtra["connector_name"])
	bindings := ResolveOrBuildShardBindings(parentExtra, parent.SourceDB, pub, slot, connector)
	if len(bindings) == 0 {
		bindings = []ShardDbBinding{{DbName: parent.SourceDB, PublicationName: pub}}
	}
	wrote := false
	for _, b := range bindings {
		dbMaps := make([]TableMapping, 0, len(maps))
		for _, m := range maps {
			if MappingAppliesToDB(m, b.DbName) {
				dbMaps = append(dbMaps, m)
			}
		}
		collections := BuildSnapshotDataCollections(schema, dbMaps)
		if len(collections) == 0 {
			continue
		}
		dataJSON, merr := json.Marshal(map[string][]string{"data-collections": collections})
		if merr != nil {
			return fmt.Errorf("marshal snapshot data-collections: %w", merr)
		}
		db, oerr := dbmig.OpenInstance(ctx, inst, o.Secret, b.DbName)
		if oerr != nil {
			return fmt.Errorf("connect db %s for snapshot signal: %w", b.DbName, oerr)
		}
		if _, serr := EnsureDebeziumSignalTableInPublication(ctx, db, defaultStr(b.PublicationName, pub), signalTable); serr != nil {
			db.Close()
			return fmt.Errorf("db %s ensure signal: %w", b.DbName, serr)
		}
		if err := InsertDebeziumSnapshotSignal(ctx, db, signalTable, signalID, string(dataJSON)); err != nil {
			db.Close()
			return fmt.Errorf("db %s insert debezium_signal: %w", b.DbName, err)
		}
		db.Close()
		o.appendLog(t, "INFO", "debezium", fmt.Sprintf("execute-snapshot written: db=%s table=%s id=%s collections=%v", b.DbName, signalTable, signalID, collections))
		wrote = true
	}
	if !wrote {
		o.appendLog(t, "INFO", "debezium", "initialization 映射在各分库无源表，跳过 execute-snapshot")
		return nil
	}
	extra["snapshot_signal_id"] = signalID
	return nil
}

func (o *Orchestrator) stepAddTableDebezium(ctx context.Context, t, parent *store.CDCTask, extra, parentExtra map[string]string, tables []string) error {
	schema := defaultStr(extra["source_schema"], defaultStr(parentExtra["source_schema"], "public"))
	pub := defaultStr(extra["publication_name"], parentExtra["publication_name"])
	slot := defaultStr(extra["slot_name"], parentExtra["slot_name"])
	connector := defaultStr(extra["connector_name"], parentExtra["connector_name"])
	bindings := ResolveOrBuildShardBindings(parentExtra, parent.SourceDB, pub, slot, connector)
	if len(bindings) == 0 {
		bindings = []ShardDbBinding{{DbName: parent.SourceDB, PublicationName: pub, SlotName: slot, ConnectorName: connector}}
	}
	for _, b := range bindings {
		name := defaultStr(b.ConnectorName, connector)
		base := o.persistConnectBase(parentExtra)
		if name == "" || base == "" {
			o.appendLog(t, "INFO", "debezium", "跳过 connector 更新 name="+name)
			continue
		}
		cfg, err := o.ConnectorConfig(ctx, parentExtra, name)
		if err != nil {
			return fmt.Errorf("读取 connector %s: %w", name, err)
		}
		if cfg == nil {
			cfg = CurrentConfigFromExtra(parentExtra)
		}
		next := MergeTableIncludeList(cfg["table.include.list"], schema, tables)
		if next == cfg["table.include.list"] {
			continue
		}
		cfg["table.include.list"] = next
		if err := o.PutConnectorConfig(ctx, parentExtra, name, cfg); err != nil {
			return err
		}
		o.restartConnectorBestEffort(ctx, t, parentExtra, name)
	}
	return nil
}

// restartConnectorBestEffort 用于 PUT config 之后：Connect 已因 task config 变化重建 Task，这里失败只告警。
func (o *Orchestrator) restartConnectorBestEffort(ctx context.Context, t *store.CDCTask, extra map[string]string, name string) {
	if err := connectRestartConnectorAndTasks(ctx, o.ResolveConnectBase(extra), name); err != nil {
		o.appendLog(t, "WARN", "debezium", "重启 connector "+name+" 失败："+err.Error())
		return
	}
	o.appendLog(t, "INFO", "debezium", "已重启 connector 及其 task："+name)
}

func (o *Orchestrator) applyAlterSchema(ctx context.Context, t *store.CDCTask) error {
	parent, err := o.parentTask(t)
	if err != nil {
		return err
	}
	extra := parseTaskExtra(t.Description)
	if extra["alter_schema_exec_mode"] == "sql_only" {
		o.appendLog(t, "INFO", "starrocks", "仅生成 SQL，跳过自动执行")
		_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
		return nil
	}
	items := parseAlterSchemaItems(extra)
	if len(items) == 0 {
		return fmt.Errorf("未指定待加列")
	}
	targetID := defaultStr(t.TargetID, parent.TargetID)
	targetDB := defaultStr(t.TargetDB, parent.TargetDB)
	if targetID == "" || targetDB == "" {
		return fmt.Errorf("目标端未配置，无法自动执行 ALTER")
	}
	inst, err := o.Store.GetInstance(targetID)
	if err != nil || inst == nil {
		return fmt.Errorf("目标实例不存在")
	}
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, targetDB)
	if err != nil {
		return fmt.Errorf("连接目标端失败: %w", err)
	}
	defer db.Close()
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	var sqls []string
	for _, item := range items {
		cols := selectedAlterColumns(item.Columns)
		stmt := BuildAlterAddColumnSQL(targetDB, item.TargetTable, cols)
		if stmt == "" {
			continue
		}
		sqls = append(sqls, stmt)
		if _, err := db.ExecContext(ctx, strings.TrimSuffix(stmt, ";")); err != nil {
			extra["error_step"] = "starrocks"
			o.appendLog(t, "ERROR", "starrocks", err.Error())
			o.saveExtra(t, extra)
			_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
			return fmt.Errorf("ALTER %s: %w", item.TargetTable, err)
		}
		o.appendLog(t, "INFO", "starrocks", stmt)
	}
	parentExtra := parseTaskExtra(parent.Description)
	mode := defaultStr(extra["debezium_mode"], parentExtra["debezium_mode"])
	brokers := o.resolveKafkaBrokers(parentExtra)
	if brokers == "" {
		brokers = o.resolveKafkaBrokers(extra)
	}
	prefix := defaultStr(extra["topic_prefix"], parentExtra["topic_prefix"])
	schema := defaultStr(extra["source_schema"], defaultStr(parentExtra["source_schema"], "public"))
	conc := extraAtoi(parentExtra["routine_load_parallelism"], 3)
	for _, item := range items {
		srCols, _ := ListSRColumns(ctx, db, targetDB, item.TargetTable)
		topic := prefix
		if topic != "" && len(item.SourceTables) > 0 {
			topic = prefix + "." + schema + "." + item.SourceTables[0]
		}
		if err := WaitSRAlterColumn(ctx, db, targetDB, item.TargetTable, 90*time.Second); err != nil {
			o.appendLog(t, "WARN", "starrocks", "等待 schema change: "+err.Error())
		}
		offset := defaultStr(extra["kafka_default_offsets"], "OFFSET_BEGINNING")
		schemaMode := defaultStr(extra["message_schema_mode"], parentExtra["message_schema_mode"])
		plan := BuildAlterMigratePlanOpts(targetDB, item.TargetTable, brokers, topic, mode, srCols, selectedAlterColumns(item.Columns), conc, offset, schemaMode, extra["env"], t.SourceDB, prefix)
		for _, stmt := range []string{plan["pause_sql"], plan["stop_sql"]} {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, strings.TrimSuffix(stmt, ";")); err != nil {
				o.appendLog(t, "WARN", "starrocks", "RL "+stmt+" : "+err.Error())
			}
		}
		if create := plan["create_sql"]; create != "" {
			if _, err := db.ExecContext(ctx, strings.TrimSuffix(create, ";")); err != nil {
				o.appendLog(t, "WARN", "starrocks", "重建 RL 失败: "+err.Error())
			} else {
				o.appendLog(t, "INFO", "starrocks", "已重建 Routine Load "+RoutineLoadNameFor(extra["env"], t.SourceDB, extra["topic_prefix"], item.TargetTable))
				sqls = append(sqls, create)
			}
		}
	}
	extra["target_sql"] = strings.Join(sqls, "\n")
	delete(extra, "error_step")
	o.saveExtra(t, extra)
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	return nil
}

func (o *Orchestrator) applyDeleteTask(ctx context.Context, t *store.CDCTask) error {
	target := t
	if strings.TrimSpace(t.ParentID) != "" {
		if parent, _ := o.Store.GetCDC(t.ParentID); parent != nil {
			target = parent
		}
	}
	if err := o.CleanupTaskResources(ctx, target); err != nil {
		o.appendLog(t, "WARN", "delete", err.Error())
	}
	extra := parseTaskExtra(target.Description)
	extra["pipeline_status"] = "deleted"
	extra["deleted"] = "true"
	o.saveExtra(target, extra)
	_ = o.Store.UpdateCDCStatus(target.ID, store.StatusDeleted, store.StatusApproved)
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusDeleted, store.StatusApproved)
	o.appendLog(t, "INFO", "delete", "已清理 Connector / Slot / Publication")
	return nil
}

func (o *Orchestrator) CleanupTaskResources(ctx context.Context, t *store.CDCTask) error {
	if t == nil {
		return fmt.Errorf("任务为空")
	}
	extra := parseTaskExtra(t.Description)
	pub := extra["publication_name"]
	slot := extra["slot_name"]
	connector := extra["connector_name"]
	bindings := ResolveOrBuildShardBindings(extra, t.SourceDB, pub, slot, connector)
	if len(bindings) == 0 {
		bindings = []ShardDbBinding{{DbName: t.SourceDB, PublicationName: pub, SlotName: slot, ConnectorName: connector}}
	}
	inst, _ := o.Store.GetInstance(t.SourceID)
	var last error
	for _, b := range bindings {
		name := defaultStr(b.ConnectorName, connector)
		if name != "" && o.persistConnectBase(extra) != "" {
			if err := o.connectAction(ctx, extra, "DELETE", "/connectors/"+name, nil); err != nil {
				o.appendLog(t, "WARN", "debezium", "删除 connector "+name+" : "+err.Error())
				last = err
			}
		}
		if inst == nil {
			continue
		}
		db, err := dbmig.OpenInstance(ctx, inst, o.Secret, b.DbName)
		if err != nil {
			o.appendLog(t, "WARN", "pg", "连接 "+b.DbName+" : "+err.Error())
			last = err
			continue
		}
		if err := DropReplicationSlot(ctx, db, b.SlotName); err != nil {
			o.appendLog(t, "WARN", "pg", "drop slot "+b.SlotName+" : "+err.Error())
			last = err
		}
		if err := DropPublication(ctx, db, b.PublicationName); err != nil {
			o.appendLog(t, "WARN", "pg", "drop publication "+b.PublicationName+" : "+err.Error())
			last = err
		}
		db.Close()
	}
	return last
}

func (o *Orchestrator) applyRemoveTable(ctx context.Context, t *store.CDCTask) error {
	parent, err := o.parentTask(t)
	if err != nil {
		return err
	}
	extra := parseTaskExtra(t.Description)
	parentExtra := parseTaskExtra(parent.Description)
	inheritParentCDCNames(extra, parentExtra)
	tables := FilterProtectedPublicationTables(splitCSV(extra["tables"]))
	if len(tables) == 0 {
		return fmt.Errorf("未指定移除表")
	}
	inst, err := o.Store.GetInstance(parent.SourceID)
	if err != nil || inst == nil {
		return fmt.Errorf("源端实例不存在")
	}
	schema := defaultStr(extra["source_schema"], defaultStr(parentExtra["source_schema"], "public"))
	pub := defaultStr(extra["publication_name"], parentExtra["publication_name"])
	slot := defaultStr(extra["slot_name"], parentExtra["slot_name"])
	connector := defaultStr(extra["connector_name"], parentExtra["connector_name"])
	bindings := ResolveOrBuildShardBindings(parentExtra, parent.SourceDB, pub, slot, connector)
	if len(bindings) == 0 {
		bindings = []ShardDbBinding{{DbName: parent.SourceDB, PublicationName: pub, SlotName: slot, ConnectorName: connector}}
	}
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	for _, b := range bindings {
		if err := o.dropPublicationTables(ctx, inst, b.DbName, schema, b.PublicationName, tables); err != nil {
			extra["error_step"] = "pg"
			o.appendLog(t, "ERROR", "pg", err.Error())
			o.saveExtra(t, extra)
			_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
			return err
		}
		name := defaultStr(b.ConnectorName, connector)
		if name != "" && o.persistConnectBase(parentExtra) != "" {
			cfg, err := o.ConnectorConfig(ctx, parentExtra, name)
			if err != nil {
				extra["error_step"] = "debezium"
				o.appendLog(t, "ERROR", "debezium", err.Error())
				o.saveExtra(t, extra)
				_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
				return err
			}
			if cfg != nil {
				next := RemoveFromTableIncludeList(cfg["table.include.list"], schema, tables)
				if next != cfg["table.include.list"] {
					cfg["table.include.list"] = next
					if err := o.PutConnectorConfig(ctx, parentExtra, name, cfg); err != nil {
						extra["error_step"] = "debezium"
						o.appendLog(t, "ERROR", "debezium", err.Error())
						o.saveExtra(t, extra)
						_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
						return err
					}
					o.restartConnectorBestEffort(ctx, t, parentExtra, name)
				}
			}
		}
	}
	targets := targetTablesForRemoved(parentExtra, tables)
	o.routineLoadActionForTables(ctx, parent, parentExtra, "STOP", targets)
	removeTablesFromParent(parent, parentExtra, tables)
	o.saveExtra(parent, parentExtra)
	delete(extra, "error_step")
	extra["pipeline_status"] = "done"
	o.saveExtra(t, extra)
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	o.appendLog(t, "INFO", "orchestrate", "remove_table 完成 "+strings.Join(tables, ","))
	return nil
}

func inheritParentCDCNames(extra, parentExtra map[string]string) {
	if extra == nil || parentExtra == nil {
		return
	}
	for _, k := range []string{"publication_name", "slot_name", "connector_name", "topic_prefix", "source_schema", "debezium_mode", "message_schema_mode", "connector_id", "connect_base_url", "shard_db", "source_db_names", "source_db_prefix", "source_scope_mode", "shard_db_bindings", "auto_create_table"} {
		if strings.TrimSpace(extra[k]) == "" && strings.TrimSpace(parentExtra[k]) != "" {
			extra[k] = parentExtra[k]
		}
	}
}

func mergeTablesIntoParent(parent *store.CDCTask, parentExtra map[string]string, tables []string, mappingsJSON string) {
	exist := map[string]struct{}{}
	cur := splitCSV(parentExtra["tables"])
	for _, t := range cur {
		exist[strings.ToLower(t)] = struct{}{}
	}
	for _, t := range tables {
		if _, ok := exist[strings.ToLower(t)]; ok {
			continue
		}
		cur = append(cur, t)
	}
	parentExtra["tables"] = strings.Join(cur, ",")
	if strings.TrimSpace(mappingsJSON) == "" {
		return
	}
	existMaps := ParseMappingsJSON(parentExtra["mappings"])
	addMaps := ParseMappingsJSON(mappingsJSON)
	for i := range addMaps {
		if strings.TrimSpace(addMaps[i].Source) == "" {
			addMaps[i].Source = "add_table"
		}
	}
	existMaps = append(existMaps, addMaps...)
	if b, err := json.Marshal(existMaps); err == nil {
		parentExtra["mappings"] = string(b)
	}
	_ = parent
}

func removeTablesFromParent(parent *store.CDCTask, parentExtra map[string]string, tables []string) {
	drop := map[string]struct{}{}
	for _, t := range tables {
		drop[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	var kept []string
	for _, t := range splitCSV(parentExtra["tables"]) {
		if _, ok := drop[strings.ToLower(t)]; ok {
			continue
		}
		kept = append(kept, t)
	}
	parentExtra["tables"] = strings.Join(kept, ",")
	maps := ParseMappingsJSON(parentExtra["mappings"])
	var next []TableMapping
	for _, m := range maps {
		if strings.EqualFold(m.Status, "deleted") {
			next = append(next, m)
			continue
		}
		var src []string
		for _, s := range m.SourceTables {
			if _, ok := drop[strings.ToLower(s)]; ok {
				continue
			}
			src = append(src, s)
		}
		if len(src) == 0 {
			m.Status = "deleted"
			if strings.TrimSpace(m.Source) == "" || m.Source == "init" {
				m.Source = "remove_table"
			}
			next = append(next, m)
			continue
		}
		m.SourceTables = src
		next = append(next, m)
	}
	if b, err := json.Marshal(next); err == nil {
		parentExtra["mappings"] = string(b)
	}
	_ = parent
}

func targetTablesForRemoved(parentExtra map[string]string, tables []string) []string {
	drop := map[string]struct{}{}
	for _, t := range tables {
		drop[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" {
			return
		}
		key := strings.ToLower(n)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, n)
	}
	for _, m := range ParseMappingsJSON(parentExtra["mappings"]) {
		if strings.EqualFold(m.Status, "deleted") {
			continue
		}
		hit := false
		if _, ok := drop[strings.ToLower(m.TargetTable)]; ok {
			hit = true
		}
		for _, s := range m.SourceTables {
			if _, ok := drop[strings.ToLower(s)]; ok {
				hit = true
				break
			}
		}
		if hit {
			add(m.TargetTable)
		}
	}
	for _, t := range tables {
		add(t)
	}
	return out
}

func parseAlterSchemaItems(extra map[string]string) []alterSchemaItem {
	if extra == nil {
		return nil
	}
	raw := strings.TrimSpace(extra["alter_schema_tables"])
	if raw != "" {
		var items []alterSchemaItem
		if json.Unmarshal([]byte(raw), &items) == nil && len(items) > 0 {
			return items
		}
	}
	cols := splitCSV(extra["alter_columns"])
	var colItems []SchemaDiffColumn
	for _, n := range cols {
		colItems = append(colItems, SchemaDiffColumn{Name: n, SRType: "STRING", Selected: true})
	}
	var out []alterSchemaItem
	for _, tb := range splitCSV(extra["tables"]) {
		out = append(out, alterSchemaItem{TargetTable: tb, SourceTables: []string{tb}, Columns: colItems})
	}
	return out
}

func selectedAlterColumns(cols []SchemaDiffColumn) []SchemaDiffColumn {
	hasSelected := false
	for _, c := range cols {
		if c.Selected {
			hasSelected = true
			break
		}
	}
	var out []SchemaDiffColumn
	for _, c := range cols {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		if hasSelected && !c.Selected {
			continue
		}
		c.Selected = true
		out = append(out, c)
	}
	return out
}
