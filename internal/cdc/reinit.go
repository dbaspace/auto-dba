package cdc

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

const (
	ReinitActionSkip    = "skip"
	ReinitActionCreate  = "create"
	ReinitActionUpdate  = "update"
	ReinitActionResume  = "resume"
	ReinitActionWarn    = "warn"
	ReinitActionUnknown = "unknown"

	SnapshotInitial = "initial"

	reinitLogPrefix = "[重新初始化] "
)

// ReinitRequest 单任务 / 批量 / 重试共用参数；SnapshotMode 为空表示沿用任务原提交值。
type ReinitRequest struct {
	SnapshotMode       string `json:"snapshot_mode"`
	ConnectorID        string `json:"connector_id"`
	ResumeRoutineLoads *bool  `json:"resume_routine_loads"`
	Operator           string `json:"-"`
	OperatorIP         string `json:"-"`
}

func (r ReinitRequest) resumeRL() bool {
	return r.ResumeRoutineLoads == nil || *r.ResumeRoutineLoads
}

type ReinitItem struct {
	Resource string `json:"resource"`
	Name     string `json:"name"`
	DbName   string `json:"db_name,omitempty"`
	Exists   bool   `json:"exists"`
	Action   string `json:"action"`
	Message  string `json:"message"`
}

type ReinitWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ReinitConfigPreview struct {
	ConnectorName string            `json:"connector_name"`
	DbName        string            `json:"db_name"`
	Config        map[string]string `json:"config"`
}

type ReinitPreviewResult struct {
	TaskID               string                `json:"task_id"`
	TaskName             string                `json:"task_name"`
	Status               string                `json:"status"`
	ConnectorID          string                `json:"connector_id"`
	ConnectBaseURLOld    string                `json:"connect_base_url_old"`
	ConnectBaseURLNew    string                `json:"connect_base_url_new"`
	SnapshotMode         string                `json:"snapshot_mode"`
	SnapshotModeOriginal string                `json:"snapshot_mode_original"`
	MappingCount         int                   `json:"mapping_count"`
	InitIncomplete       bool                  `json:"init_incomplete"`
	Items                []ReinitItem          `json:"items"`
	Warnings             []ReinitWarning       `json:"warnings"`
	Configs              []ReinitConfigPreview `json:"configs"`
}

// ConnectorPlan 单个 shard binding 将要下发的 Connector；Config 含真实密码，不得原样返回前端。
type ConnectorPlan struct {
	ConnectorName   string
	DbName          string
	PublicationName string
	SlotName        string
	Tables          []string
	Config          map[string]string
}

// reinitIdentityKeys 由任务元数据决定、不沿用模板（UI 改动）的 Connector 配置项。
var reinitIdentityKeys = []string{
	"database.hostname", "database.port", "database.user", "database.password", "database.dbname",
	"publication.name", "slot.name", "table.include.list", "snapshot.mode",
}

// ResolveReinitSnapshotMode 未传取任务原值（原值为空用 never）；显式只允许 never / initial。
func ResolveReinitSnapshotMode(req, original string) (string, error) {
	req = strings.ToLower(strings.TrimSpace(req))
	switch req {
	case "":
		if o := strings.TrimSpace(original); o != "" {
			return o, nil
		}
		return SnapshotNever, nil
	case SnapshotNever, SnapshotInitial:
		return req, nil
	default:
		return "", fmt.Errorf("snapshot_mode 仅支持 never / initial，或留空沿用任务原配置")
	}
}

// ActiveMappings 排除 deleted 的表映射（重新初始化 / 编排按原提交配置取全部有效映射）。
func ActiveMappings(maps []TableMapping) []TableMapping {
	out := make([]TableMapping, 0, len(maps))
	for _, m := range maps {
		if strings.EqualFold(strings.TrimSpace(m.Status), "deleted") {
			continue
		}
		out = append(out, m)
	}
	return out
}

// InitIncomplete 初始化编排曾在四步中途失败（或已登记待补齐），重新初始化需按表补齐 StarRocks。
func InitIncomplete(extra map[string]string) bool {
	if extra == nil {
		return false
	}
	if extra["init_starrocks_pending"] == "true" {
		return true
	}
	switch strings.TrimSpace(extra["error_step"]) {
	case "pg", "kafka", "debezium", "starrocks":
		return true
	}
	return false
}

func taskBindings(t *store.CDCTask, extra map[string]string) []ShardDbBinding {
	pub := extra["publication_name"]
	slot := extra["slot_name"]
	connector := defaultStr(extra["connector_name"], pub)
	srcDB := ""
	if t != nil {
		srcDB = t.SourceDB
	}
	return ResolveOrBuildShardBindings(extra, srcDB, pub, slot, connector)
}

type connectorPlanInput struct {
	Extra        map[string]string
	SourceDB     string
	SourceEngine string
	PGHost       string
	PGPort       int
	PGUser       string
	PGPassword   string
	SnapshotMode string
}

// BuildTaskConnectorPlans 按任务元数据为每个 shard binding 生成 Connector 配置（注入源实例真实密码）。
func BuildTaskConnectorPlans(st *store.DB, secretKey string, t *store.CDCTask, extra map[string]string, snapshotMode string) ([]ConnectorPlan, error) {
	if t == nil {
		return nil, fmt.Errorf("任务为空")
	}
	inst, err := st.GetInstance(t.SourceID)
	if err != nil || inst == nil {
		return nil, fmt.Errorf("源端实例不存在")
	}
	pwd := ""
	if strings.TrimSpace(inst.Password) != "" {
		if pwd, err = secret.Decrypt(secretKey, inst.Password); err != nil {
			return nil, fmt.Errorf("解密源端实例密码失败: %w", err)
		}
	}
	srcEng := NormalizeCDCSourceEngine(inst.DBType)
	if extra != nil {
		if v := strings.TrimSpace(extra["source_engine"]); v != "" {
			srcEng = NormalizeCDCSourceEngine(v)
		}
		extra["source_engine"] = srcEng
	}
	return buildConnectorPlans(connectorPlanInput{
		Extra:        extra,
		SourceDB:     t.SourceDB,
		SourceEngine: srcEng,
		PGHost:       defaultStr(strings.TrimSpace(inst.Host), strings.TrimSpace(inst.DomainName)),
		PGPort:       inst.Port,
		PGUser:       inst.Username,
		PGPassword:   pwd,
		SnapshotMode: snapshotMode,
	})
}

func buildConnectorPlans(in connectorPlanInput) ([]ConnectorPlan, error) {
	extra := in.Extra
	if extra == nil {
		extra = map[string]string{}
	}
	srcEng := NormalizeCDCSourceEngine(in.SourceEngine)
	if srcEng == "" {
		srcEng = SourceEnginePostgres
	}
	isMySQL := srcEng == SourceEngineMySQL
	if !isMySQL {
		if strings.TrimSpace(extra["publication_name"]) == "" || strings.TrimSpace(extra["slot_name"]) == "" {
			return nil, fmt.Errorf("任务未登记 publication / slot 名称")
		}
	} else if strings.TrimSpace(extra["topic_prefix"]) == "" {
		return nil, fmt.Errorf("MySQL CDC 任务未登记 topic_prefix")
	}
	maps := ActiveMappings(ParseMappingsJSON(extra["mappings"]))
	tables := splitCSV(extra["tables"])
	if len(maps) == 0 && len(tables) == 0 {
		return nil, fmt.Errorf("任务没有有效的表映射，无法重新初始化")
	}
	mode := defaultStr(strings.TrimSpace(extra["debezium_mode"]), ModeAfterOnly)
	schema := defaultStr(extra["source_schema"], "public")
	if isMySQL && (schema == "" || schema == "public") {
		schema = ""
	}
	port := in.PGPort
	if port <= 0 {
		if isMySQL {
			port = 3306
		} else {
			port = 5432
		}
	}
	snapshot := defaultStr(strings.TrimSpace(in.SnapshotMode), defaultStr(extra["snapshot_mode"], SnapshotNever))
	template := CurrentConfigFromExtra(extra)
	signal := ResolveSignalTable(extra)

	var plans []ConnectorPlan
	for _, b := range taskBindings(&store.CDCTask{SourceDB: in.SourceDB}, extra) {
		var dbMaps []TableMapping
		for _, m := range maps {
			if MappingAppliesToDB(m, b.DbName) {
				dbMaps = append(dbMaps, m)
			}
		}
		if len(maps) > 0 && len(dbMaps) == 0 {
			continue
		}
		plain, shards := SplitMappings(dbMaps, tables)
		generated := BuildConfig(mode, DebeziumBuildParams{
			ConnectorName:          b.ConnectorName,
			SourceEngine:           srcEng,
			PGHost:                 in.PGHost,
			PGPort:                 port,
			PGUser:                 in.PGUser,
			PGPassword:             in.PGPassword,
			PGDbName:               b.DbName,
			Schema:                 schema,
			Tables:                 plain,
			ShardGroups:            shards,
			PublicationName:        b.PublicationName,
			SlotName:               b.SlotName,
			TopicPrefix:            extra["topic_prefix"],
			SnapshotMode:           snapshot,
			TopicPartitions:        extraAtoi(extra["kafka_topic_partitions"], 0),
			TopicReplicationFactor: extraAtoi(extra["kafka_topic_replication_factor"], 0),
			FilterDeleteEvents:     ResolveFilterDeleteEvents(mode, extra["filter_delete_events"] == "true"),
			MySQLServerID:          int64(extraAtoi(extra["mysql_server_id"], 5400)),
		})
		if !isMySQL {
			generated["signal.data.collection"] = signal
		}
		cfg := generated
		if len(template) > 0 {
			cfg = cloneStringMap(template)
			for k, v := range generated {
				if _, ok := cfg[k]; !ok {
					cfg[k] = v
				}
			}
			for _, k := range reinitIdentityKeys {
				cfg[k] = generated[k]
			}
		}
		delete(cfg, "name")
		if err := validateAndNormalizeConnectorCfg(cfg); err != nil {
			return nil, fmt.Errorf("connector %s 配置校验失败: %w", b.ConnectorName, err)
		}
		plans = append(plans, ConnectorPlan{
			ConnectorName:   b.ConnectorName,
			DbName:          b.DbName,
			PublicationName: b.PublicationName,
			SlotName:        b.SlotName,
			Tables:          TablesForShardDB(dbMaps, tables, b.DbName),
			Config:          cfg,
		})
	}
	if len(plans) == 0 {
		return nil, fmt.Errorf("任务没有有效的表映射，无法重新初始化")
	}
	return plans, nil
}

// hubConnectorCopy 写回 extra.debezium_config 的副本：密码脱敏，snapshot.mode 保留任务原值。
func hubConnectorCopy(plans []ConnectorPlan, extra map[string]string) map[string]string {
	if len(plans) == 0 {
		return nil
	}
	primary := plans[0]
	want := strings.TrimSpace(extra["connector_name"])
	for _, p := range plans {
		if want != "" && p.ConnectorName == want {
			primary = p
			break
		}
	}
	out := cloneStringMap(primary.Config)
	for k := range out {
		if IsSensitiveConfigKey(k) {
			out[k] = MaskedPassword
		}
	}
	orig := CurrentConfigFromExtra(extra)["snapshot.mode"]
	out["snapshot.mode"] = defaultStr(orig, defaultStr(extra["snapshot_mode"], SnapshotNever))
	return out
}

func maskedPlanConfigs(plans []ConnectorPlan) []ReinitConfigPreview {
	out := make([]ReinitConfigPreview, 0, len(plans))
	for _, p := range plans {
		out = append(out, ReinitConfigPreview{ConnectorName: p.ConnectorName, DbName: p.DbName, Config: MaskConfigMap(p.Config)})
	}
	return out
}

func kafkaHostSet(s string) string {
	var parts []string
	for _, p := range strings.Split(NormalizeKafkaBootstrap(s), ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			parts = append(parts, p)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func (o *Orchestrator) taskKafkaHost(extra map[string]string) string {
	if v := strings.TrimSpace(extra["kafka_bootstrap_servers"]); v != "" {
		return v
	}
	if id := strings.TrimSpace(extra["connector_id"]); id != "" && o.Store != nil {
		if c, _ := o.Store.GetConnector(id); c != nil {
			return c.KafkaHost
		}
	}
	return ""
}

// validateRebind 换绑前校验：新服务存在且 Kafka 与任务一致。connectorID 为空或未变化返回 nil。
func (o *Orchestrator) validateRebind(extra map[string]string, connectorID string) (*store.Connector, error) {
	connectorID = strings.TrimSpace(connectorID)
	if connectorID == "" || connectorID == "0" || connectorID == strings.TrimSpace(extra["connector_id"]) {
		return nil, nil
	}
	c, err := o.Store.GetConnector(connectorID)
	if err != nil || c == nil {
		return nil, fmt.Errorf("Debezium 服务不存在：%s", connectorID)
	}
	if strings.TrimSpace(c.DBZHost) == "" {
		return nil, fmt.Errorf("Debezium 服务 %s 未配置 dbz_host", connectorID)
	}
	taskKafka := o.taskKafkaHost(extra)
	if taskKafka != "" && strings.TrimSpace(c.KafkaHost) != "" && kafkaHostSet(taskKafka) != kafkaHostSet(c.KafkaHost) {
		return nil, fmt.Errorf("新 Debezium 服务的 Kafka（%s）与任务 Kafka（%s）不一致，不能换绑", c.KafkaHost, taskKafka)
	}
	return c, nil
}

func applyRebind(extra map[string]string, c *store.Connector) {
	if extra == nil || c == nil {
		return
	}
	extra["connector_id"] = c.ID
	extra["connect_base_url"] = NormalizeConnectBaseURL(c.DBZHost)
	delete(extra, "worker_url")
	if m := strings.TrimSpace(c.MessageSchemaMode); m != "" {
		extra["message_schema_mode"] = m
	}
}

// RebindConnector 换绑 Debezium 服务：更新 init 任务及其子任务 extra 中的 connector_id / connect_base_url。
func (o *Orchestrator) RebindConnector(t *store.CDCTask, extra map[string]string, connectorID string) (bool, error) {
	c, err := o.validateRebind(extra, connectorID)
	if err != nil || c == nil {
		return false, err
	}
	applyRebind(extra, c)
	o.saveExtra(t, extra)
	children, _ := o.Store.ListCDC("", "")
	for i := range children {
		ch := &children[i]
		if strings.TrimSpace(ch.ParentID) != t.ID {
			continue
		}
		ce := parseTaskExtra(ch.Description)
		applyRebind(ce, c)
		o.saveExtra(ch, ce)
	}
	return true, nil
}

func (o *Orchestrator) reinitLog(t *store.CDCTask, level, msg, operator string) {
	o.appendLogFull(t, "", level, "reinit", reinitLogPrefix+msg, "", operator)
}

// ReinitPreview 只读：逐项探测将要补齐的资源，不做任何修改。
func (o *Orchestrator) ReinitPreview(ctx context.Context, taskID string, req ReinitRequest) (*ReinitPreviewResult, error) {
	t, err := o.Store.GetCDC(taskID)
	if err != nil || t == nil {
		return nil, fmt.Errorf("任务不存在")
	}
	extra := parseTaskExtra(t.Description)
	res := &ReinitPreviewResult{
		TaskID: t.ID, TaskName: t.Name, Status: t.Status,
		SnapshotModeOriginal: defaultStr(extra["snapshot_mode"], SnapshotNever),
		InitIncomplete:       InitIncomplete(extra),
		Items:                []ReinitItem{},
		Warnings:             []ReinitWarning{},
		Configs:              []ReinitConfigPreview{},
	}
	res.ConnectBaseURLOld = NormalizeConnectBaseURL(defaultStr(extra["connect_base_url"], extra["worker_url"]))
	if res.ConnectBaseURLOld == "" {
		res.ConnectBaseURLOld = o.ResolveConnectBase(extra)
	}
	conn, err := o.validateRebind(extra, req.ConnectorID)
	if err != nil {
		return nil, err
	}
	if conn != nil {
		applyRebind(extra, conn)
		res.Warnings = append(res.Warnings, ReinitWarning{Code: "connector_rebind", Message: fmt.Sprintf("换绑 Debezium 服务：%s → %s", res.ConnectBaseURLOld, NormalizeConnectBaseURL(conn.DBZHost))})
	}
	res.ConnectorID = extra["connector_id"]
	res.ConnectBaseURLNew = o.ResolveConnectBase(extra)
	snap, err := ResolveReinitSnapshotMode(req.SnapshotMode, extra["snapshot_mode"])
	if err != nil {
		return nil, err
	}
	res.SnapshotMode = snap
	if snap == SnapshotInitial {
		res.Warnings = append(res.Warnings, ReinitWarning{Code: "snapshot_initial", Message: "生效的 snapshot_mode=initial：Connect 无 offset 时会对全部表重做全量快照"})
	}
	maps := ActiveMappings(ParseMappingsJSON(extra["mappings"]))
	res.MappingCount = len(maps)
	if res.MappingCount == 0 {
		res.MappingCount = len(splitCSV(extra["tables"]))
	}
	plans, err := BuildTaskConnectorPlans(o.Store, o.Secret, t, extra, snap)
	if err != nil {
		return nil, err
	}
	res.Configs = maskedPlanConfigs(plans)

	o.previewPG(ctx, t, extra, plans, res)
	o.previewKafka(ctx, extra, res)
	o.previewConnectors(ctx, res.ConnectBaseURLNew, plans, res)
	o.previewStarRocks(ctx, t, extra, req.resumeRL(), res)
	return res, nil
}

func (res *ReinitPreviewResult) add(it ReinitItem) { res.Items = append(res.Items, it) }

func (res *ReinitPreviewResult) warn(code, msg string) {
	res.Warnings = append(res.Warnings, ReinitWarning{Code: code, Message: msg})
}

func (o *Orchestrator) previewPG(ctx context.Context, t *store.CDCTask, extra map[string]string, plans []ConnectorPlan, res *ReinitPreviewResult) {
	inst, _ := o.Store.GetInstance(t.SourceID)
	schema := defaultStr(extra["source_schema"], "public")
	for _, p := range plans {
		if inst == nil {
			res.add(ReinitItem{Resource: "publication", Name: p.PublicationName, DbName: p.DbName, Action: ReinitActionUnknown, Message: "源端实例不存在"})
			res.warn("probe_failed", "源端实例不存在")
			continue
		}
		db, err := dbmig.OpenInstance(ctx, inst, o.Secret, p.DbName)
		if err != nil {
			res.add(ReinitItem{Resource: "publication", Name: p.PublicationName, DbName: p.DbName, Action: ReinitActionUnknown, Message: "连接源端失败：" + err.Error()})
			res.add(ReinitItem{Resource: "slot", Name: p.SlotName, DbName: p.DbName, Action: ReinitActionUnknown, Message: "连接源端失败"})
			res.warn("probe_failed", fmt.Sprintf("分库 %s 连接失败：%v", p.DbName, err))
			continue
		}
		exists, _ := PublicationExists(ctx, db, p.PublicationName)
		if !exists {
			res.add(ReinitItem{Resource: "publication", Name: p.PublicationName, DbName: p.DbName, Action: ReinitActionCreate, Message: fmt.Sprintf("不存在，将创建（%d 张表）", len(p.Tables))})
		} else {
			have, _ := ListPublicationTables(ctx, db, p.PublicationName)
			missing := missingPublicationTables(schema, p.Tables, have)
			if len(missing) == 0 {
				res.add(ReinitItem{Resource: "publication", Name: p.PublicationName, DbName: p.DbName, Exists: true, Action: ReinitActionSkip, Message: "已存在且表齐全，跳过"})
			} else {
				res.add(ReinitItem{Resource: "publication", Name: p.PublicationName, DbName: p.DbName, Exists: true, Action: ReinitActionUpdate, Message: "已存在，将补充缺失表：" + strings.Join(missing, ",")})
			}
		}
		if info, serr := GetReplicationSlot(ctx, db, p.SlotName); serr == nil && info != nil {
			res.add(ReinitItem{Resource: "slot", Name: p.SlotName, DbName: p.DbName, Exists: true, Action: ReinitActionSkip, Message: fmt.Sprintf("已存在，复用位点（active=%v）", info.Active)})
		} else {
			res.add(ReinitItem{Resource: "slot", Name: p.SlotName, DbName: p.DbName, Action: ReinitActionCreate, Message: "不存在，将创建（从当前位点开始捕获）"})
			res.warn("slot_missing", fmt.Sprintf("slot %s 不存在，新建后从当前位点开始捕获，此前的变更会缺失，可按需走 add_table 初始化补数", p.SlotName))
		}
		db.Close()
	}
}

func (o *Orchestrator) previewKafka(ctx context.Context, extra map[string]string, res *ReinitPreviewResult) {
	brokers := o.resolveKafkaBrokers(extra)
	schema := defaultStr(extra["source_schema"], "public")
	topics := CollectTaskKafkaTopics(extra["topic_prefix"], schema, ActiveMappings(ParseMappingsJSON(extra["mappings"])), splitCSV(extra["tables"]))
	if brokers == "" || len(topics) == 0 {
		return
	}
	exist, err := ProbeKafkaTopics(ctx, brokers, topics)
	for _, name := range topics {
		switch {
		case err != nil:
			res.add(ReinitItem{Resource: "kafka_topic", Name: name, Action: ReinitActionUnknown, Message: "探测失败，执行时再判断"})
		case exist[name]:
			res.add(ReinitItem{Resource: "kafka_topic", Name: name, Exists: true, Action: ReinitActionSkip, Message: "已存在，跳过"})
		default:
			res.add(ReinitItem{Resource: "kafka_topic", Name: name, Action: ReinitActionCreate, Message: "不存在，将创建"})
		}
	}
	if err != nil {
		res.warn("probe_failed", "Kafka 探测失败："+err.Error())
	}
}

func (o *Orchestrator) previewConnectors(ctx context.Context, base string, plans []ConnectorPlan, res *ReinitPreviewResult) {
	if base == "" {
		for _, p := range plans {
			res.add(ReinitItem{Resource: "connector", Name: p.ConnectorName, DbName: p.DbName, Action: ReinitActionWarn, Message: "未解析到 Debezium Connect 地址，请先在「配置 → 连接器」配置 dbz_host"})
		}
		return
	}
	InvalidateConnectorList(base)
	existing, err := ListConnectConnectors(ctx, base)
	if err != nil {
		res.warn("probe_failed", "Debezium Connect 不可达："+err.Error())
	}
	for _, p := range plans {
		switch {
		case err != nil:
			res.add(ReinitItem{Resource: "connector", Name: p.ConnectorName, DbName: p.DbName, Action: ReinitActionUnknown, Message: "Connect 不可达，执行时再判断"})
		case existing[p.ConnectorName]:
			res.add(ReinitItem{Resource: "connector", Name: p.ConnectorName, DbName: p.DbName, Exists: true, Action: ReinitActionUpdate, Message: "已存在，将按任务元数据覆盖配置（覆盖前保存版本快照）"})
			res.warn("connector_override", fmt.Sprintf("Connector %s 已存在，配置会被覆盖（可在配置历史恢复）", p.ConnectorName))
		default:
			res.add(ReinitItem{Resource: "connector", Name: p.ConnectorName, DbName: p.DbName, Action: ReinitActionCreate, Message: "Connect 上不存在，将创建"})
		}
	}
}

func missingPublicationTables(schema string, want, have []string) []string {
	seen := map[string]bool{}
	for _, h := range have {
		seen[strings.ToLower(QualifyTableInclude(schema, h))] = true
	}
	var out []string
	for _, w := range want {
		if !seen[strings.ToLower(QualifyTableInclude(schema, w))] {
			out = append(out, w)
		}
	}
	return out
}

// Reinit 同步执行重新初始化（由 runner 在后台调用；调用方已把状态置为 reinitializing）。
func (o *Orchestrator) Reinit(ctx context.Context, taskID string, req ReinitRequest) (err error) {
	t, gerr := o.Store.GetCDC(taskID)
	if gerr != nil || t == nil {
		return fmt.Errorf("任务不存在")
	}
	extra := parseTaskExtra(t.Description)
	op := req.Operator
	step := "prepare"
	defer func() {
		if err == nil {
			return
		}
		extra["error_step"] = "reinit"
		extra["error_message"] = fmt.Sprintf("重新初始化失败[%s]：%s", StepLabel(step), err.Error())
		extra["pipeline_status"] = "failed"
		o.saveExtra(t, extra)
		o.reinitLog(t, "ERROR", fmt.Sprintf("步骤[%s]失败：%s", StepLabel(step), err.Error()), op)
		_ = o.Store.UpdateCDCStatus(t.ID, store.StatusFailed, t.ApprovalStatus)
	}()

	if InitIncomplete(extra) {
		extra["init_starrocks_pending"] = "true"
	}
	snap, err := ResolveReinitSnapshotMode(req.SnapshotMode, extra["snapshot_mode"])
	if err != nil {
		return err
	}
	o.reinitLog(t, "INFO", fmt.Sprintf("开始：snapshot_mode=%s resume_routine_loads=%v 初始化未完成=%v", snap, req.resumeRL(), extra["init_starrocks_pending"] == "true"), op)

	step = "rebind"
	rebound, err := o.RebindConnector(t, extra, req.ConnectorID)
	if err != nil {
		return err
	}
	base := o.persistConnectBase(extra)
	if rebound {
		o.reinitLog(t, "INFO", "已换绑 Debezium 服务 connector_id="+extra["connector_id"]+" connect="+base, op)
	} else if base != "" {
		o.reinitLog(t, "INFO", "按注册表刷新 Connect 地址："+base, op)
	}
	if base == "" {
		return fmt.Errorf("未解析到 Debezium Connect 地址，请先在「配置 → 连接器」配置 dbz_host")
	}
	o.saveExtra(t, extra)

	step = "debezium"
	plans, err := BuildTaskConnectorPlans(o.Store, o.Secret, t, extra, snap)
	if err != nil {
		return err
	}

	step = "pg"
	if err = o.reinitPG(ctx, t, extra, plans, op); err != nil {
		return err
	}
	step = "kafka"
	if err = o.stepKafka(ctx, t, extra); err != nil {
		return err
	}
	step = "debezium"
	if err = o.reinitConnectors(ctx, t, extra, base, plans, op, req.OperatorIP); err != nil {
		return err
	}
	o.saveExtra(t, extra)
	step = "starrocks"
	if err = o.reinitStarRocks(ctx, t, extra, req.resumeRL(), op); err != nil {
		return err
	}

	delete(extra, "error_step")
	delete(extra, "error_message")
	delete(extra, "init_starrocks_pending")
	extra["pipeline_status"] = "running"
	o.saveExtra(t, extra)
	o.reinitLog(t, "INFO", "全部资源已补齐，任务恢复 running", op)
	return o.Store.UpdateCDCStatus(t.ID, store.StatusRunning, t.ApprovalStatus)
}

func (o *Orchestrator) reinitPG(ctx context.Context, t *store.CDCTask, extra map[string]string, plans []ConnectorPlan, op string) error {
	inst, err := o.Store.GetInstance(t.SourceID)
	if err != nil || inst == nil {
		return fmt.Errorf("源端实例不存在")
	}
	schema := defaultStr(extra["source_schema"], "public")
	signalTable := ResolveSignalTable(extra)
	for _, p := range plans {
		if err := o.reinitPGBinding(ctx, t, inst, schema, p, op); err != nil {
			return fmt.Errorf("分库 %s: %w", p.DbName, err)
		}
		if err := o.ensureSignalOnDB(ctx, inst, p.DbName, p.PublicationName, signalTable); err != nil {
			return fmt.Errorf("分库 %s signal: %w", p.DbName, err)
		}
		_ = o.Store.UpsertSlotMeta(&store.SyncSlot{
			Host: inst.Host, Port: inst.Port, SlotName: p.SlotName, Database: p.DbName, Active: true, Plugin: "pgoutput",
		})
	}
	return nil
}

func (o *Orchestrator) reinitPGBinding(ctx context.Context, t *store.CDCTask, inst *store.Instance, schema string, p ConnectorPlan, op string) error {
	db, err := dbmig.OpenInstance(ctx, inst, o.Secret, p.DbName)
	if err != nil {
		return fmt.Errorf("连接源端失败: %w", err)
	}
	defer db.Close()
	exists, err := PublicationExists(ctx, db, p.PublicationName)
	if err != nil {
		return fmt.Errorf("查询 publication 失败: %w", err)
	}
	if !exists {
		q := qualifyPGTables(schema, p.Tables)
		if len(q) == 0 {
			return fmt.Errorf("未指定同步表")
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE PUBLICATION %s FOR TABLE %s`, quotePGIdent(p.PublicationName), strings.Join(q, ", "))); err != nil {
			return fmt.Errorf("创建 publication 失败: %w", err)
		}
		o.reinitLog(t, "INFO", fmt.Sprintf("publication %s（%s）已创建", p.PublicationName, p.DbName), op)
	} else {
		have, _ := ListPublicationTables(ctx, db, p.PublicationName)
		if missing := missingPublicationTables(schema, p.Tables, have); len(missing) > 0 {
			stmt := fmt.Sprintf(`ALTER PUBLICATION %s ADD TABLE %s`, quotePGIdent(p.PublicationName), strings.Join(qualifyPGTables(schema, missing), ", "))
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("publication 补表失败: %w", err)
			}
			o.reinitLog(t, "INFO", fmt.Sprintf("publication %s（%s）已补表：%s", p.PublicationName, p.DbName, strings.Join(missing, ",")), op)
		} else {
			o.reinitLog(t, "INFO", fmt.Sprintf("publication %s（%s）已存在且表齐全，跳过", p.PublicationName, p.DbName), op)
		}
	}
	if _, serr := GetReplicationSlot(ctx, db, p.SlotName); serr == nil {
		o.reinitLog(t, "INFO", fmt.Sprintf("slot %s（%s）已存在，复用位点", p.SlotName, p.DbName), op)
		return nil
	}
	slotSQL := fmt.Sprintf(`SELECT pg_create_logical_replication_slot(%s, 'pgoutput')`, quotePGLiteral(p.SlotName))
	if _, err := db.ExecContext(ctx, slotSQL); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return fmt.Errorf("创建 replication slot 失败: %w", err)
	}
	o.reinitLog(t, "WARN", fmt.Sprintf("slot %s（%s）不存在，已新建：从当前位点开始捕获，此前的变更会缺失", p.SlotName, p.DbName), op)
	return nil
}

func (o *Orchestrator) reinitConnectors(ctx context.Context, t *store.CDCTask, extra map[string]string, base string, plans []ConnectorPlan, op, ip string) error {
	InvalidateConnectorList(base)
	defer InvalidateConnectorList(base)
	snapshotSaved := false
	for _, p := range plans {
		if connectConnectorExists(ctx, base, p.ConnectorName) {
			if !snapshotSaved {
				if before, err := connectGetConfig(ctx, base, p.ConnectorName); err == nil && len(before) > 0 {
					if _, err := SaveConnectorConfigVersionIfChanged(o.Store, VersionTaskID(t), before, VersionSourceReinit, op, ip); err != nil {
						o.reinitLog(t, "WARN", "保存 Connector 配置版本快照失败："+err.Error(), op)
					}
				}
				snapshotSaved = true
			}
			if err := connectPutConfig(ctx, base, p.ConnectorName, p.Config); err != nil {
				return fmt.Errorf("覆盖 connector %s 配置: %w", p.ConnectorName, explainDebeziumConnectError(err, p.Config))
			}
			o.reinitLog(t, "INFO", fmt.Sprintf("connector %s 已存在，已按任务元数据覆盖配置", p.ConnectorName), op)
			continue
		}
		if err := EnsureDebeziumConnector(ctx, base, p.ConnectorName, p.Config); err != nil {
			return err
		}
		o.reinitLog(t, "INFO", fmt.Sprintf("connector %s 已创建（db=%s slot=%s）", p.ConnectorName, p.DbName, p.SlotName), op)
	}
	if strings.TrimSpace(extra["connector_name"]) == "" {
		extra["connector_name"] = plans[0].ConnectorName
	}
	extra["debezium_config"] = MarshalConfigMap(hubConnectorCopy(plans, extra))
	return nil
}
