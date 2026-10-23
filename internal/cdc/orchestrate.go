package cdc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

type Orchestrator struct {
	Store  *store.DB
	Secret string
	Cfg    config.CDCConfig
}

func NewOrchestrator(st *store.DB, secret string, cfg config.CDCConfig) *Orchestrator {
	return &Orchestrator{Store: st, Secret: secret, Cfg: cfg}
}

type PipelineLog struct {
	ID        int64  `json:"id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	MappingID string `json:"mapping_id,omitempty"`
	At        string `json:"at"`
	CreatedAt string `json:"created_at,omitempty"`
	Level     string `json:"level"`
	LogLevel  string `json:"log_level,omitempty"`
	Step      string `json:"step"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	Operator  string `json:"operator,omitempty"`
}

func StepLabel(step string) string {
	switch strings.TrimSpace(step) {
	case "pg":
		return "PG Publication/Slot"
	case "mysql":
		return "MySQL Binlog 预检"
	case "debezium":
		return "Debezium Connector"
	case "kafka":
		return "Kafka Topic"
	case "starrocks":
		return "StarRocks 建表/Routine Load"
	case "orchestrate":
		return "编排"
	case "create":
		return "创建任务"
	case "manual":
		return "人工操作"
	case "pause":
		return "暂停"
	case "delete":
		return "删除"
	case "reinit":
		return "重新初始化"
	case "prepare":
		return "准备"
	case "rebind":
		return "换绑 Debezium 服务"
	default:
		return step
	}
}

func stepSuccessLog(step string) string {
	switch step {
	case "pg":
		return "pg publication & slot created"
	case "mysql":
		return "mysql binlog precheck ok"
	case "kafka":
		return "kafka topics ensured"
	case "debezium":
		return "debezium connector created"
	case "starrocks":
		return "starrocks tables & routine loads created"
	default:
		return step + " done"
	}
}

func (o *Orchestrator) markOrchestrating(t *store.CDCTask, on bool) {
	if o == nil || t == nil {
		return
	}
	extra := parseTaskExtra(t.Description)
	if on {
		extra["orchestrating"] = "true"
	} else {
		delete(extra, "orchestrating")
	}
	o.saveExtra(t, extra)
	if on && t.Status != "" {
		_ = o.Store.UpdateCDCStatus(t.ID, t.Status, t.ApprovalStatus)
	}
}

func (o *Orchestrator) Apply(ctx context.Context, taskID, fromStep string) error {
	t, err := o.Store.GetCDC(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	if !cdcTaskApproved(t) {
		return fmt.Errorf("CDC 任务须审批通过后才能执行")
	}
	if ctx.Value(orchLockCtxKey{}) == nil {
		extra := parseTaskExtra(t.Description)
		return o.withOrchestrateLocks(ctx, t.SourceID, o.resolveKafkaBrokers(extra), func(ctx context.Context) error {
			return o.Apply(ctx, taskID, fromStep)
		})
	}
	o.markOrchestrating(t, true)
	defer o.markOrchestrating(t, false)
	switch strings.TrimSpace(t.TaskType) {
	case "alter_schema":
		return o.applyAlterSchema(ctx, t)
	case "remove_table":
		return o.applyRemoveTable(ctx, t)
	case "add_table":
		return o.applyAddTable(ctx, t, fromStep)
	case "delete_task":
		return o.applyDeleteTask(ctx, t)
	}
	extra := parseTaskExtra(t.Description)
	srcEng := o.resolveCDCSourceEngine(t, extra)
	extra["source_engine"] = srcEng
	steps := []string{"pg", "kafka", "debezium", "starrocks"}
	if srcEng == SourceEngineMySQL {
		steps = []string{"mysql", "kafka", "debezium", "starrocks"}
	}
	start := 0
	fromStep = strings.ToLower(strings.TrimSpace(fromStep))
	if extra["error_step"] != "" && fromStep == "" {
		fromStep = extra["error_step"]
	}
	if fromStep == "reinit" {
		// 上次重新初始化失败：按默认参数（snapshot 沿用原值、恢复 RL、沿用当前绑定）重做，而不是重跑创建编排。
		if _, err := o.acquireReinit(taskID); err != nil {
			return err
		}
		defer reinitRunning.Delete(taskID)
		return o.Reinit(ctx, taskID, ReinitRequest{})
	}
	for i, s := range steps {
		if fromStep != "" && s == fromStep {
			start = i
			break
		}
	}
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	o.appendLog(t, "INFO", "orchestrate", "init pipeline start")

	for _, step := range steps[start:] {
		o.appendLog(t, "INFO", step, fmt.Sprintf("步骤[%s]开始执行", StepLabel(step)))
		if err := o.runStep(ctx, t, extra, step); err != nil {
			extra["error_step"] = step
			o.appendLog(t, "ERROR", step, fmt.Sprintf("步骤[%s]执行失败：%s", StepLabel(step), err.Error()))
			o.saveExtra(t, extra)
			_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
			return err
		}
		o.appendLog(t, "INFO", step, fmt.Sprintf("步骤[%s]执行成功：%s", StepLabel(step), stepSuccessLog(step)))
	}
	delete(extra, "error_step")
	extra["pipeline_status"] = "running"
	o.saveExtra(t, extra)
	_ = o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	o.appendLog(t, "INFO", "orchestrate", "全部步骤执行成功，CDC 同步任务已进入 running")
	return nil
}

func (o *Orchestrator) Pause(ctx context.Context, taskID string) error {
	t, err := o.Store.GetCDC(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	extra := parseTaskExtra(t.Description)
	base := o.persistConnectBase(extra)
	for _, connName := range o.taskConnectorNames(t, extra) {
		if base != "" && connName != "" {
			if err := o.connectAction(ctx, extra, "PUT", "/connectors/"+connName+"/pause", nil); err != nil {
				o.appendLog(t, "WARN", "debezium", "暂停 connector "+connName+" 失败: "+err.Error())
			} else {
				o.appendLog(t, "INFO", "debezium", "已暂停 connector "+connName)
			}
		}
	}
	o.routineLoadAction(ctx, t, extra, "PAUSE")
	extra["pipeline_status"] = "paused"
	o.appendLog(t, "INFO", "pause", "任务已暂停")
	o.saveExtra(t, extra)
	return o.Store.UpdateCDCStatus(t.ID, store.StatusPaused, t.ApprovalStatus)
}

func (o *Orchestrator) Resume(ctx context.Context, taskID string) error {
	t, err := o.Store.GetCDC(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	if !cdcTaskApproved(t) {
		return fmt.Errorf("CDC 任务须审批通过后才能执行")
	}
	extra := parseTaskExtra(t.Description)
	name := extra["connector_name"]
	base := o.persistConnectBase(extra)
	if base != "" && name != "" {
		for _, connName := range o.taskConnectorNames(t, extra) {
			if connName == "" {
				continue
			}
			_ = o.connectAction(ctx, extra, "PUT", "/connectors/"+connName+"/resume", nil)
			o.appendLog(t, "INFO", "debezium", "已恢复 connector "+connName)
		}
		o.routineLoadAction(ctx, t, extra, "RESUME")
		extra["pipeline_status"] = "running"
		o.saveExtra(t, extra)
		return o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
	}
	return o.Apply(ctx, taskID, extra["error_step"])
}

func (o *Orchestrator) Restart(ctx context.Context, taskID string) error {
	t, err := o.Store.GetCDC(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	if !cdcTaskApproved(t) {
		return fmt.Errorf("CDC 任务须审批通过后才能执行")
	}
	switch strings.TrimSpace(t.TaskType) {
	case "alter_schema", "remove_table", "add_table", "delete_task":
		return o.Apply(ctx, taskID, "debezium")
	}
	extra := parseTaskExtra(t.Description)
	base := o.persistConnectBase(extra)
	names := o.taskConnectorNames(t, extra)
	if base == "" || len(names) == 0 {
		return o.Apply(ctx, taskID, "debezium")
	}
	for _, name := range names {
		if !connectConnectorExists(ctx, base, name) {
			o.appendLog(t, "INFO", "debezium", "connector "+name+" 不存在，从 Debezium 步骤重新编排")
			return o.Apply(ctx, taskID, "debezium")
		}
	}
	// 不写 error_step：编排已走完的任务若标记 debezium，下次续跑会重复 CREATE ROUTINE LOAD。
	fail := func(err error) error {
		o.appendLog(t, "ERROR", "debezium", "重启失败："+err.Error())
		o.saveExtra(t, extra)
		_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
		return err
	}
	wasPaused := t.Status == store.StatusPaused
	for _, name := range names {
		if wasPaused {
			if err := o.connectAction(ctx, extra, "PUT", "/connectors/"+name+"/resume", nil); err != nil {
				return fail(fmt.Errorf("恢复 connector %s: %w", name, err))
			}
		}
		if err := connectRestartConnectorAndTasks(ctx, base, name); err != nil {
			return fail(err)
		}
		o.appendLog(t, "INFO", "debezium", "已重启 connector 及其 task："+name)
	}
	waitCtx, cancel := context.WithTimeout(ctx, connectorSettleTimeout)
	defer cancel()
	for _, name := range names {
		err := connectWaitRunning(waitCtx, base, name, connectorSettleTimeout)
		switch {
		case err == nil:
			o.appendLog(t, "INFO", "debezium", "connector "+name+" 及其 task 已 RUNNING")
		case errors.Is(err, errNotSettled):
			o.appendLog(t, "WARN", "debezium", fmt.Sprintf("connector %s 在 %s 内未全部进入 RUNNING，请稍后在详情页确认状态", name, connectorSettleTimeout))
		default:
			return fail(err)
		}
	}
	if extra["error_step"] != "" {
		// 上次编排未走完（例如 Routine Load 未建成），重启后从失败步骤续跑。
		return o.Apply(ctx, taskID, "")
	}
	o.routineLoadAction(ctx, t, extra, "RESUME")
	extra["pipeline_status"] = "running"
	o.saveExtra(t, extra)
	o.appendLog(t, "INFO", "restart", "任务已重启")
	return o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
}

// connectorSettleTimeout 须小于 handler 的 45s 超时，给重启请求与 Routine Load 留出余量。
const connectorSettleTimeout = 25 * time.Second

func (o *Orchestrator) runStep(ctx context.Context, t *store.CDCTask, extra map[string]string, step string) error {
	switch step {
	case "pg":
		return o.stepPG(ctx, t, extra)
	case "mysql":
		return o.stepMySQL(ctx, t, extra)
	case "kafka":
		return o.stepKafka(ctx, t, extra)
	case "debezium":
		return o.stepDebezium(ctx, t, extra)
	case "starrocks":
		if extra["auto_create_table"] == "false" || strings.TrimSpace(t.TargetID) == "" {
			o.appendLog(t, "INFO", "starrocks", "手动 DDL 或未配置目标端，跳过自动建表 / Routine Load")
			return nil
		}
		sqlText, err := PreviewTargetSQL(ctx, o.Store, o.Secret, t)
		if err != nil {
			o.appendLog(t, "WARN", "starrocks", "预览目标 SQL 失败: "+err.Error())
			return nil
		}
		extra["target_sql"] = sqlText
		if !o.Cfg.Enabled {
			o.appendLog(t, "INFO", "starrocks", "CDC 未启用，仅生成目标 SQL 预览")
			return nil
		}
		return o.applyTargetSQL(ctx, t, sqlText)
	}
	return nil
}

func (o *Orchestrator) resolveCDCSourceEngine(t *store.CDCTask, extra map[string]string) string {
	if extra != nil {
		if v := strings.TrimSpace(extra["source_engine"]); v != "" {
			return NormalizeCDCSourceEngine(v)
		}
	}
	if t != nil && o != nil && o.Store != nil {
		if inst, _ := o.Store.GetInstance(t.SourceID); inst != nil {
			return NormalizeCDCSourceEngine(inst.DBType)
		}
	}
	return SourceEnginePostgres
}

// stepMySQL MySQL 源：校验 binlog/GTID，并补齐 connector/topic 命名（无 Publication/Slot）。
func (o *Orchestrator) stepMySQL(ctx context.Context, t *store.CDCTask, extra map[string]string) error {
	inst, err := o.Store.GetInstance(t.SourceID)
	if err != nil || inst == nil {
		return fmt.Errorf("源端实例不存在")
	}
	connector := extra["connector_name"]
	if extra["topic_prefix"] == "" || connector == "" {
		_, _, topic := defaultTaskNames(extra["env"], t.SourceDB, t.Name)
		if extra["topic_prefix"] == "" {
			extra["topic_prefix"] = topic
		}
		if connector == "" {
			connector = sanitizeIdent(defaultStr(t.Name, t.SourceDB))
			extra["connector_name"] = connector
		}
	}
	if strings.TrimSpace(extra["mysql_server_id"]) == "" {
		extra["mysql_server_id"] = "5400"
	}
	// 兼容分片绑定：MySQL 无 slot/pub，仍登记 binding 以便 debezium 多库 fanout
	pub := defaultStr(extra["publication_name"], "mysql_"+sanitizeIdent(t.SourceDB))
	slot := defaultStr(extra["slot_name"], "mysql_"+sanitizeIdent(t.SourceDB))
	extra["publication_name"] = pub
	extra["slot_name"] = slot
	tables := splitCSV(extra["tables"])
	maps := ParseMappingsJSON(extra["mappings"])
	if len(tables) == 0 && len(maps) == 0 {
		return fmt.Errorf("未指定同步表")
	}
	if err := ValidateMappingShardScopes(maps, ResolveShardDBs(extra, t.SourceDB)); err != nil {
		return err
	}
	bindings := ResolveOrBuildShardBindings(extra, t.SourceDB, pub, slot, connector)
	PersistShardBindings(extra, bindings)

	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, t.SourceDB)
	if err != nil {
		return fmt.Errorf("连接 MySQL 源端失败: %w", err)
	}
	defer db.Close()
	var logBin, binlogFormat, gtidMode sql.NullString
	var srcServerID sql.NullInt64
	_ = db.QueryRowContext(ctx, `SELECT @@log_bin, @@binlog_format, @@gtid_mode, @@server_id`).Scan(&logBin, &binlogFormat, &gtidMode, &srcServerID)
	if logBin.Valid && !strings.EqualFold(logBin.String, "1") && !strings.EqualFold(logBin.String, "ON") {
		return fmt.Errorf("源端未开启 binlog（log_bin=%s）", logBin.String)
	}
	if binlogFormat.Valid && !strings.EqualFold(binlogFormat.String, "ROW") {
		return fmt.Errorf("binlog_format 须为 ROW，当前=%s", binlogFormat.String)
	}
	wantSID := extraAtoi(extra["mysql_server_id"], 5400)
	if srcServerID.Valid && srcServerID.Int64 > 0 && int64(wantSID) == srcServerID.Int64 {
		return fmt.Errorf("mysql_server_id=%d 与源端 @@server_id 冲突，请为 Debezium 指定不同的 database.server.id", wantSID)
	}
	if err := o.checkMySQLServerIDConflict(t, wantSID); err != nil {
		return err
	}
	o.appendLog(t, "INFO", "mysql", fmt.Sprintf("binlog 预检通过 log_bin=%s format=%s gtid=%s server_id=%d connector=%s debezium_server_id=%d",
		logBin.String, binlogFormat.String, gtidMode.String, srcServerID.Int64, connector, wantSID))
	return nil
}

// checkMySQLServerIDConflict 同源实例上其它活跃 CDC 任务不得复用同一 Debezium server.id。
func (o *Orchestrator) checkMySQLServerIDConflict(cur *store.CDCTask, wantSID int) error {
	if o == nil || o.Store == nil || cur == nil || wantSID <= 0 {
		return nil
	}
	list, err := o.Store.ListCDC("", "")
	if err != nil {
		return nil
	}
	active := map[string]bool{
		"running": true, "creating": true, "provisioning": true, "init": true, "paused": true,
	}
	for i := range list {
		other := list[i]
		if other.ID == cur.ID {
			continue
		}
		if other.SourceID != cur.SourceID {
			continue
		}
		if !active[strings.ToLower(strings.TrimSpace(other.Status))] {
			continue
		}
		ex := ParseTaskExtra(other.Description)
		otherSID := extraAtoi(ex["mysql_server_id"], 0)
		if otherSID == 0 {
			continue
		}
		if otherSID == wantSID {
			return fmt.Errorf("mysql_server_id=%d 已被同源任务 %s（%s）占用，请更换 database.server.id",
				wantSID, other.Name, other.ID)
		}
	}
	return nil
}

func (o *Orchestrator) stepPG(ctx context.Context, t *store.CDCTask, extra map[string]string) error {
	inst, err := o.Store.GetInstance(t.SourceID)
	if err != nil || inst == nil {
		return fmt.Errorf("源端实例不存在")
	}
	schema := extra["source_schema"]
	if schema == "" {
		schema = "public"
	}
	pub := extra["publication_name"]
	slot := extra["slot_name"]
	connector := extra["connector_name"]
	if pub == "" || slot == "" || connector == "" || extra["topic_prefix"] == "" {
		pub2, slot2, topic := defaultTaskNames(extra["env"], t.SourceDB, t.Name)
		if pub == "" {
			pub = pub2
		}
		if slot == "" {
			slot = slot2
		}
		if extra["topic_prefix"] == "" {
			extra["topic_prefix"] = topic
		}
		if connector == "" {
			connector = sanitizeIdent(defaultStr(t.Name, t.SourceDB))
			extra["connector_name"] = connector
		}
		extra["publication_name"] = pub
		extra["slot_name"] = slot
	}
	tables := splitCSV(extra["tables"])
	maps := ParseMappingsJSON(extra["mappings"])
	if len(tables) == 0 && len(maps) == 0 {
		return fmt.Errorf("未指定同步表")
	}
	if err := ValidateMappingShardScopes(maps, ResolveShardDBs(extra, t.SourceDB)); err != nil {
		return err
	}
	bindings := ResolveOrBuildShardBindings(extra, t.SourceDB, pub, slot, connector)
	PersistShardBindings(extra, bindings)
	signalTable := ResolveSignalTable(extra)
	// 同一批分库共享 Kafka，必须等上一个库的 Publication/Slot 完成后再处理下一个。
	return runShardStepsSerial(len(bindings), func(i int) error {
		b := bindings[i]
		dbTables := TablesForShardDB(maps, tables, b.DbName)
		if len(dbTables) == 0 {
			o.appendLog(t, "INFO", "pg", "分库 "+b.DbName+" 无映射表，跳过")
			return nil
		}
		if err := o.ensurePublicationAndSlot(ctx, inst, b.DbName, schema, b.PublicationName, b.SlotName, dbTables, false); err != nil {
			return fmt.Errorf("分库 %s: %w", b.DbName, err)
		}
		if err := o.ensureSignalOnDB(ctx, inst, b.DbName, b.PublicationName, signalTable); err != nil {
			return fmt.Errorf("分库 %s signal: %w", b.DbName, err)
		}
		_ = o.Store.UpsertSlotMeta(&store.SyncSlot{
			Host: inst.Host, Port: inst.Port, SlotName: b.SlotName, Database: b.DbName, Active: true, Plugin: "pgoutput",
		})
		o.appendLog(t, "INFO", "pg", "publication="+b.PublicationName+" slot="+b.SlotName+" db="+b.DbName)
		return nil
	})
}

func (o *Orchestrator) ensurePublicationAndSlot(ctx context.Context, inst *store.Instance, dbName, schema, pub, slot string, tables []string, addOnly bool) error {
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, dbName)
	if err != nil {
		return fmt.Errorf("连接源端失败: %w", err)
	}
	defer db.Close()
	applyPGOrchestrationTimeouts(db)
	qTables := qualifyPGTables(schema, tables)
	if len(qTables) == 0 {
		return fmt.Errorf("未指定同步表")
	}
	if addOnly {
		have, herr := ListPublicationTables(ctx, db, pub)
		if herr != nil {
			return fmt.Errorf("查询 publication 成员失败: %w", herr)
		}
		missing := missingPublicationTables(schema, tables, have)
		if len(missing) == 0 {
			return nil
		}
		pubSQL := fmt.Sprintf(`ALTER PUBLICATION %s ADD TABLE %s`, quotePGIdent(pub), strings.Join(qualifyPGTables(schema, missing), ", "))
		if _, err := db.ExecContext(ctx, pubSQL); err != nil {
			return fmt.Errorf("ALTER PUBLICATION 失败: %w", err)
		}
		return nil
	}
	pubSQL := fmt.Sprintf(`CREATE PUBLICATION %s FOR TABLE %s`, quotePGIdent(pub), strings.Join(qTables, ", "))
	if _, err := db.ExecContext(ctx, pubSQL); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return fmt.Errorf("创建 publication 失败: %w", err)
	}
	if err := createLogicalSlot(ctx, db, slot); err != nil {
		return err
	}
	return nil
}

func (o *Orchestrator) dropPublicationTables(ctx context.Context, inst *store.Instance, dbName, schema, pub string, tables []string) error {
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, dbName)
	if err != nil {
		return fmt.Errorf("连接源端失败: %w", err)
	}
	defer db.Close()
	qTables := qualifyPGTables(schema, tables)
	if len(qTables) == 0 {
		return nil
	}
	stmt := fmt.Sprintf(`ALTER PUBLICATION %s DROP TABLE %s`, quotePGIdent(pub), strings.Join(qTables, ", "))
	if _, err := db.ExecContext(ctx, stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "does not exist") {
		return fmt.Errorf("ALTER PUBLICATION DROP 失败: %w", err)
	}
	return nil
}

func qualifyPGTables(schema string, tables []string) []string {
	if schema == "" {
		schema = "public"
	}
	qTables := make([]string, 0, len(tables))
	for _, tb := range tables {
		tb = strings.TrimSpace(tb)
		if tb == "" {
			continue
		}
		if !strings.Contains(tb, ".") {
			tb = schema + "." + tb
		}
		qTables = append(qTables, quotePGIdent(tb))
	}
	return qTables
}

// stepDebezium 每个 shard binding 一个 Connector，下发时注入源实例真实密码；extra 副本保持脱敏。
func (o *Orchestrator) stepDebezium(ctx context.Context, t *store.CDCTask, extra map[string]string) error {
	plans, err := BuildTaskConnectorPlans(o.Store, o.Secret, t, extra, extra["snapshot_mode"])
	if err != nil {
		return err
	}
	if strings.TrimSpace(extra["connector_name"]) == "" {
		extra["connector_name"] = plans[0].ConnectorName
	}
	extra["debezium_config"] = MarshalConfigMap(hubConnectorCopy(plans, extra))
	extra["debezium_template"] = TemplateName(defaultStr(extra["debezium_mode"], ModeAfterOnly))
	base := o.persistConnectBase(extra)
	if base == "" {
		o.appendLog(t, "INFO", "debezium", "未解析到 Debezium Connect 地址（连接器 dbz_host / 任务覆盖 / cdc.debezium_url 均空），仅生成 connector 配置")
		return nil
	}
	defer InvalidateConnectorList(base)
	for _, p := range plans {
		if err := EnsureDebeziumConnector(ctx, base, p.ConnectorName, p.Config); err != nil {
			return fmt.Errorf("connector %s: %w", p.ConnectorName, err)
		}
		o.appendLog(t, "INFO", "debezium", fmt.Sprintf("connector %s 已就绪（db=%s slot=%s）", p.ConnectorName, p.DbName, p.SlotName))
	}
	return nil
}

func (o *Orchestrator) applyTargetSQL(ctx context.Context, t *store.CDCTask, sqlText string) error {
	if strings.TrimSpace(t.TargetID) == "" || strings.TrimSpace(sqlText) == "" {
		return nil
	}
	inst, err := o.Store.GetInstance(t.TargetID)
	if err != nil || inst == nil {
		return fmt.Errorf("目标实例不存在")
	}
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, t.TargetDB)
	if err != nil {
		return fmt.Errorf("连接目标端失败: %w", err)
	}
	defer db.Close()
	for _, stmt := range strings.Split(sqlText, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if isCreateRoutineLoad(stmt) && createAlreadyExists(err) {
				if retryErr := recreateRoutineLoad(ctx, db, routineLoadDB(stmt), routineLoadName(stmt), stmt); retryErr != nil {
					return fmt.Errorf("执行目标 SQL 失败: %w", retryErr)
				}
				continue
			}
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "CREATE") && createAlreadyExists(err) {
				continue
			}
			return fmt.Errorf("执行目标 SQL 失败: %w", err)
		}
	}
	return nil
}

func (o *Orchestrator) connectAction(ctx context.Context, extra map[string]string, method, path string, body any) error {
	base := o.ResolveConnectBase(extra)
	raw, code, err := connectDo(ctx, base, method, path, body)
	if err != nil {
		return err
	}
	if code >= 300 && code != http.StatusConflict {
		return fmt.Errorf("debezium %s %s -> %d %s", method, path, code, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (o *Orchestrator) ConnectorConfig(ctx context.Context, extra map[string]string, name string) (map[string]string, error) {
	if o == nil || strings.TrimSpace(name) == "" {
		return nil, nil
	}
	base := o.ResolveConnectBase(extra)
	if base == "" {
		return nil, nil
	}
	return connectGetConfig(ctx, base, name)
}

func (o *Orchestrator) PutConnectorConfig(ctx context.Context, extra map[string]string, name string, cfg map[string]string) error {
	if o == nil {
		return nil
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("connector 名称为空")
	}
	base := o.ResolveConnectBase(extra)
	if base == "" {
		return fmt.Errorf("未配置 Connect 地址：请在任务中选择 Debezium 连接器，或在 config.yaml 填写 cdc.debezium_url")
	}
	return connectPutConfig(ctx, base, name, cfg)
}

func (o *Orchestrator) ConnectorStatus(ctx context.Context, extra map[string]string, name string) (map[string]any, error) {
	if strings.TrimSpace(name) == "" {
		return map[string]any{"ok": false, "message": "connector 名称为空"}, nil
	}
	base := o.ResolveConnectBase(extra)
	if base == "" {
		return map[string]any{"ok": false, "message": "Debezium 未配置"}, nil
	}
	return connectGetStatus(ctx, base, name)
}

func (o *Orchestrator) resolveKafkaBrokers(extra map[string]string) string {
	host := ""
	if id := strings.TrimSpace(extra["connector_id"]); id != "" && o != nil && o.Store != nil {
		if c, _ := o.Store.GetConnector(id); c != nil {
			host = c.KafkaHost
		}
	}
	return ResolveKafkaBootstrap(extra["kafka_bootstrap_servers"], host, o.Cfg.KafkaBrokers)
}

func PreviewTargetSQL(ctx context.Context, st *store.DB, secret string, t *store.CDCTask) (string, error) {
	items, err := PreviewTargetSQLItems(ctx, st, secret, t)
	if err != nil {
		return "", err
	}
	sqlText := JoinTargetSQL(items)
	if sqlText == "" {
		return "", fmt.Errorf("未生成目标 SQL")
	}
	return sqlText, nil
}

func PreviewTargetSQLItems(ctx context.Context, st *store.DB, secret string, t *store.CDCTask) ([]TargetSQLItem, error) {
	if t == nil {
		return nil, fmt.Errorf("任务为空")
	}
	extra := parseTaskExtra(t.Description)
	schema := extra["source_schema"]
	if schema == "" {
		schema = "public"
	}
	tables := splitCSV(extra["tables"])
	maps := ParseMappingsJSON(extra["mappings"])
	if len(tables) == 0 && len(maps) == 0 {
		return nil, fmt.Errorf("未指定同步表")
	}
	inst, err := st.GetInstance(t.SourceID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("源端实例不存在")
	}
	db, err := dbmig.OpenInstance(ctx, inst, secret, t.SourceDB)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	need := map[string]struct{}{}
	for _, m := range maps {
		for _, s := range m.SourceTables {
			if s = strings.TrimSpace(s); s != "" {
				need[s] = struct{}{}
			}
		}
	}
	for _, tb := range tables {
		need[tb] = struct{}{}
	}
	colsBy := map[string][]pgCol{}
	pksBy := map[string][]string{}
	for tb := range need {
		cols, pks, err := pgColumns(ctx, db, schema, tb)
		if err != nil {
			return nil, err
		}
		colsBy[tb] = cols
		pksBy[tb] = pks
	}
	connHost, schemaMode := "", extra["message_schema_mode"]
	if id := strings.TrimSpace(extra["connector_id"]); id != "" {
		if c, _ := st.GetConnector(id); c != nil {
			connHost = c.KafkaHost
			if schemaMode == "" {
				schemaMode = c.MessageSchemaMode
			}
		}
	}
	globalDP := DefaultSRDynamicPartitionProps
	if st != nil {
		if raw, _ := st.GetGlobalArg("sync_sr_dynamic_partition_defaults"); strings.TrimSpace(raw) != "" {
			if parsed := ParseJSONStringMap(raw); len(parsed) > 0 {
				globalDP = MergeStringMaps(DefaultSRDynamicPartitionProps, parsed)
			}
		}
	}
	items := BuildTargetSQLItems(TargetSQLParams{
		TargetDB:           t.TargetDB,
		KeyModel:           extra["key_model"],
		Buckets:            extraAtoi(extra["sr_auto_buckets"], 10),
		ReplicationNum:     extraAtoi(extra["sr_replication_num"], 1),
		RoutineLoadConc:    extraAtoi(extra["routine_load_parallelism"], extraAtoi(extra["sync_sr_routine_load_desired_concurrent"], 3)),
		KafkaBrokers:       ResolveKafkaBootstrap(extra["kafka_bootstrap_servers"], connHost, ""),
		TopicPrefix:        extra["topic_prefix"],
		Env:                extra["env"],
		SourceDB:           t.SourceDB,
		SourceSchema:       schema,
		DebeziumMode:       extra["debezium_mode"],
		MessageSchemaMode:  schemaMode,
		AutoCreate:         extra["auto_create_table"] != "false",
		DynamicPartitionOn: extra["sr_dynamic_partition_enable"] == "true",
		TaskPartitionCol:   extra["sr_partition_column"],
		TaskPartitionProps: ParseJSONStringMap(extra["sr_dynamic_partition"]),
		GlobalPartition:    globalDP,
		Mappings:           maps,
		FallbackTables:     tables,
		TaskType:           t.TaskType,
		SQLOverrides:       ParseSQLOverrides(extra["sql_overrides"]),
		KafkaDefaultOffset: extra["kafka_default_offsets"],
	}, colsBy, pksBy)
	return items, nil
}

type pgCol struct {
	Name         string
	DataType     string
	UDT          string
	IsNullable   bool
	CharMaxLen   int
	NumPrecision int
	NumScale     int
	IsIdentity   bool
	Comment      string
	DefaultExpr  string
}

func pgColumns(ctx context.Context, db *sql.DB, schema, table string) ([]pgCol, []string, error) {
	rows, err := db.QueryContext(ctx, `
SELECT c.column_name,
       c.data_type,
       c.udt_name,
       (c.is_nullable = 'YES') AS is_nullable,
       COALESCE(c.character_maximum_length, 0) AS char_max_len,
       COALESCE(c.numeric_precision, 0) AS num_precision,
       COALESCE(c.numeric_scale, 0) AS num_scale,
       (c.is_identity = 'YES') AS is_identity,
       col_description(pgc.oid, a.attnum) AS column_comment,
       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '') AS default_expr
FROM information_schema.columns c
JOIN pg_class pgc ON pgc.relname = c.table_name
JOIN pg_namespace n ON n.oid = pgc.relnamespace AND n.nspname = c.table_schema
JOIN pg_attribute a ON a.attrelid = pgc.oid AND a.attname = c.column_name
  AND a.attnum > 0 AND NOT a.attisdropped
LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
WHERE c.table_schema = $1 AND c.table_name = $2
ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var cols []pgCol
	for rows.Next() {
		var c pgCol
		var comment sql.NullString
		if err := rows.Scan(&c.Name, &c.DataType, &c.UDT, &c.IsNullable, &c.CharMaxLen, &c.NumPrecision, &c.NumScale, &c.IsIdentity, &comment, &c.DefaultExpr); err != nil {
			return nil, nil, err
		}
		if comment.Valid {
			c.Comment = strings.TrimSpace(comment.String)
		}
		c.DefaultExpr = strings.TrimSpace(c.DefaultExpr)
		if !c.IsIdentity && pgDefaultLooksLikeIdentity(c.DefaultExpr) {
			c.IsIdentity = true
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	pkRows, err := db.QueryContext(ctx, `
		SELECT a.attname FROM pg_index i
		JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=ANY(i.indkey)
		JOIN pg_class c ON c.oid=i.indrelid
		JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE i.indisprimary AND n.nspname=$1 AND c.relname=$2`, schema, table)
	if err != nil {
		return cols, nil, nil
	}
	defer pkRows.Close()
	var pks []string
	for pkRows.Next() {
		var n string
		if err := pkRows.Scan(&n); err == nil {
			pks = append(pks, n)
		}
	}
	return cols, pks, nil
}

func (o *Orchestrator) appendLog(t *store.CDCTask, level, step, msg string) {
	o.appendLogFull(t, "", level, step, msg, "", "")
}

func (o *Orchestrator) appendLogFull(t *store.CDCTask, mappingID, level, step, msg, detail, operator string) {
	if t == nil || o == nil || o.Store == nil {
		return
	}
	_ = o.Store.InsertCDCLog(&store.CDCSyncLog{
		TaskID:    t.ID,
		MappingID: mappingID,
		Level:     level,
		Step:      step,
		Message:   msg,
		Detail:    detail,
		Operator:  operator,
	})
}

func PipelineLogFromStore(it store.CDCSyncLog) PipelineLog {
	at := it.CreatedAt
	return PipelineLog{
		ID:        it.ID,
		TaskID:    it.TaskID,
		MappingID: it.MappingID,
		At:        at,
		CreatedAt: at,
		Level:     it.Level,
		LogLevel:  it.Level,
		Step:      it.Step,
		Message:   it.Message,
		Detail:    it.Detail,
		Operator:  it.Operator,
	}
}

func (o *Orchestrator) saveExtra(t *store.CDCTask, extra map[string]string) {
	if t == nil {
		return
	}
	text, _ := splitCDCDesc(t.Description)
	b, _ := json.Marshal(extra)
	t.Description = strings.TrimSpace(text + "\n-- auto-dba-cdc:" + string(b))
	_ = o.Store.UpdateCDCDescription(t.ID, t.Description)
}

func (o *Orchestrator) stepKafka(ctx context.Context, t *store.CDCTask, extra map[string]string) error {
	brokers := o.resolveKafkaBrokers(extra)
	if brokers == "" {
		o.appendLog(t, "INFO", "kafka", "Kafka 未配置（连接器 / 覆盖 / 全局均空），跳过预创建 topic")
		return nil
	}
	if extra["kafka_bootstrap_servers"] == "" {
		extra["kafka_bootstrap_servers"] = brokers
	}
	schema := defaultStr(extra["source_schema"], "public")
	topics := CollectTaskKafkaTopics(extra["topic_prefix"], schema, ParseMappingsJSON(extra["mappings"]), splitCSV(extra["tables"]))
	if len(topics) == 0 {
		o.appendLog(t, "INFO", "kafka", "无 topic 需预创建 brokers="+brokers+" topic_prefix="+extra["topic_prefix"])
		return nil
	}
	parts := extraAtoi(extra["kafka_topic_partitions"], 3)
	rf := extraAtoi(extra["kafka_topic_replication_factor"], 1)
	if err := EnsureKafkaTopics(ctx, brokers, topics, parts, rf); err != nil {
		return err
	}
	o.appendLog(t, "INFO", "kafka", fmt.Sprintf("已确保 %d 个 Kafka topic（partitions=%d, replication=%d）: %s", len(topics), parts, rf, strings.Join(topics, ",")))
	return nil
}

func (o *Orchestrator) taskConnectorNames(t *store.CDCTask, extra map[string]string) []string {
	pub := extra["publication_name"]
	slot := extra["slot_name"]
	connector := extra["connector_name"]
	if connector == "" {
		connector = extra["publication_name"]
	}
	srcDB := ""
	if t != nil {
		srcDB = t.SourceDB
	}
	bindings := ResolveOrBuildShardBindings(extra, srcDB, pub, slot, connector)
	seen := map[string]struct{}{}
	var out []string
	for _, b := range bindings {
		name := strings.TrimSpace(b.ConnectorName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 && connector != "" {
		out = []string{connector}
	}
	return out
}

func (o *Orchestrator) routineLoadAction(ctx context.Context, t *store.CDCTask, extra map[string]string, action string) {
	if t == nil || strings.TrimSpace(t.TargetID) == "" || extra["auto_create_table"] == "false" {
		return
	}
	action = strings.ToUpper(strings.TrimSpace(action))
	if action != "PAUSE" && action != "RESUME" && action != "STOP" {
		return
	}
	inst, err := o.Store.GetInstance(t.TargetID)
	if err != nil || inst == nil {
		o.appendLog(t, "WARN", "starrocks", "目标实例不存在，跳过 Routine Load "+action)
		return
	}
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, t.TargetDB)
	if err != nil {
		o.appendLog(t, "WARN", "starrocks", "连接目标端失败，跳过 Routine Load "+action+": "+err.Error())
		return
	}
	defer db.Close()
	maps := ParseMappingsJSON(extra["mappings"])
	names := map[string]struct{}{}
	add := func(table string) {
		table = strings.TrimSpace(table)
		if table == "" {
			return
		}
		names[RoutineLoadNameForTask(t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"], table)] = struct{}{}
	}
	for _, m := range maps {
		if strings.EqualFold(m.Status, "deleted") {
			continue
		}
		add(m.TargetTable)
	}
	for _, tb := range splitCSV(extra["tables"]) {
		add(tb)
	}
	o.execRoutineLoadNames(ctx, db, t, t.TargetDB, names, action)
}

func (o *Orchestrator) routineLoadActionForTables(ctx context.Context, t *store.CDCTask, extra map[string]string, action string, tables []string) {
	if t == nil || strings.TrimSpace(t.TargetID) == "" || extra["auto_create_table"] == "false" {
		return
	}
	action = strings.ToUpper(strings.TrimSpace(action))
	if action != "PAUSE" && action != "RESUME" && action != "STOP" {
		return
	}
	inst, err := o.Store.GetInstance(t.TargetID)
	if err != nil || inst == nil {
		o.appendLog(t, "WARN", "starrocks", "目标实例不存在，跳过 Routine Load "+action)
		return
	}
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, t.TargetDB)
	if err != nil {
		o.appendLog(t, "WARN", "starrocks", "连接目标端失败，跳过 Routine Load "+action+": "+err.Error())
		return
	}
	defer db.Close()
	names := map[string]struct{}{}
	for _, tb := range tables {
		tb = strings.TrimSpace(tb)
		if tb == "" {
			continue
		}
		names[RoutineLoadNameForTask(t.TaskType, extra["env"], t.SourceDB, extra["topic_prefix"], tb)] = struct{}{}
	}
	o.execRoutineLoadNames(ctx, db, t, t.TargetDB, names, action)
}

func (o *Orchestrator) execRoutineLoadNames(ctx context.Context, db *sql.DB, t *store.CDCTask, targetDB string, names map[string]struct{}, action string) {
	for rl := range names {
		stmt := fmt.Sprintf("%s ROUTINE LOAD FOR `%s`.`%s`", action, t.TargetDB, rl)
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			o.appendLog(t, "WARN", "starrocks", stmt+" : "+err.Error())
		} else {
			o.appendLog(t, "INFO", "starrocks", stmt)
		}
	}
}

func (o *Orchestrator) ensureSignalOnDB(ctx context.Context, inst *store.Instance, dbName, pub, signalTable string) error {
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, dbName)
	if err != nil {
		return err
	}
	defer db.Close()
	created, err := EnsureDebeziumSignalTableInPublication(ctx, db, pub, signalTable)
	if err != nil {
		return err
	}
	if created {
		return nil
	}
	return nil
}

func extraAtoi(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func ParseTaskExtra(desc string) map[string]string {
	return parseTaskExtra(desc)
}

func parseTaskExtra(desc string) map[string]string {
	out := map[string]string{}
	_, extra := splitCDCDesc(desc)
	if extra == "" {
		return out
	}
	var raw map[string]any
	if json.Unmarshal([]byte(extra), &raw) != nil {
		return out
	}
	for k, v := range raw {
		switch x := v.(type) {
		case string:
			out[k] = x
		case bool:
			if x {
				out[k] = "true"
			} else {
				out[k] = "false"
			}
		case []any:
			allStr := true
			parts := make([]string, 0, len(x))
			for _, it := range x {
				s, ok := it.(string)
				if !ok {
					allStr = false
					break
				}
				if strings.TrimSpace(s) != "" {
					parts = append(parts, s)
				}
			}
			if allStr {
				out[k] = strings.Join(parts, ",")
			} else {
				b, _ := json.Marshal(v)
				out[k] = string(b)
			}
		default:
			b, _ := json.Marshal(v)
			out[k] = string(b)
		}
	}
	return out
}

func splitCDCDesc(desc string) (text, extra string) {
	const p = "-- auto-dba-cdc:"
	i := strings.Index(desc, p)
	if i < 0 {
		return strings.TrimSpace(desc), ""
	}
	return strings.TrimSpace(desc[:i]), strings.TrimSpace(desc[i+len(p):])
}

func cdcTaskApproved(t *store.CDCTask) bool {
	if t == nil {
		return false
	}
	if t.ApprovalStatus == store.StatusPending || t.ApprovalStatus == store.StatusRejected || t.ApprovalStatus == store.StatusCanceled {
		return false
	}
	if t.ApprovalStatus == "" || t.ApprovalStatus == "none" {
		return t.IssueID == ""
	}
	return t.ApprovalStatus == store.StatusApproved
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' }) {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func quotePGIdent(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		p = strings.Trim(p, `"`)
		parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
	}
	return strings.Join(parts, ".")
}

func quotePGLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
