package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/store"
)

type PipelineNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	State  string `json:"state,omitempty"`
}

type PipelinePG struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type PipelinePublication struct {
	Name        string `json:"name"`
	Exists      bool   `json:"exists"`
	TableCount  int    `json:"table_count"`
	Unreachable bool   `json:"unreachable,omitempty"`
	Message     string `json:"message,omitempty"`
}

type PipelineSlot struct {
	Name                string `json:"name"`
	Active              bool   `json:"active"`
	LagBytes            int64  `json:"lag_bytes"`
	ReplicationLagBytes int64  `json:"replication_lag_bytes"`
	RestartLSN          string `json:"restart_lsn,omitempty"`
	Message             string `json:"message,omitempty"`
}

type PipelineKafkaTopic struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
}

type PipelineKafka struct {
	State      string `json:"state"`
	Brokers    string `json:"brokers,omitempty"`
	Message    string `json:"message,omitempty"`
	TopicCount int    `json:"topic_count"`
	AllExist   bool   `json:"all_exist"`
}

type PipelineRoutineLoad struct {
	Name   string `json:"name"`
	Table  string `json:"table"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type PipelineStarRocks struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

func AggregatePipelineStatus(ctx context.Context, st *store.DB, secret string, o *Orchestrator, t *store.CDCTask) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	extra := parseTaskExtra(t.Description)
	pub := extra["publication_name"]
	slot := extra["slot_name"]
	connector := extra["connector_name"]
	prefix := extra["topic_prefix"]
	schema := defaultStr(extra["source_schema"], "public")
	maps := PipelineActiveMappings(ParseMappingsJSON(extra["mappings"]))
	tables := splitCSV(extra["tables"])
	bindings := ResolveOrBuildShardBindings(extra, t.SourceDB, pub, slot, connector)
	if len(bindings) == 0 {
		bindings = []ShardDbBinding{{DbName: t.SourceDB, PublicationName: pub, SlotName: slot, ConnectorName: connector}}
	}

	pg := &PipelinePG{State: "UNKNOWN"}
	publication := &PipelinePublication{Name: pub}
	var slotInfo *PipelineSlot
	pgHost, pgPort := "", 0
	if inst, _ := st.GetInstance(t.SourceID); inst != nil && strings.TrimSpace(t.SourceID) != "" {
		pgHost = defaultStr(strings.TrimSpace(inst.Host), strings.TrimSpace(inst.DomainName))
		pgPort = inst.Port
		totalTables := 0
		allExist := true
		checked := 0
		var lastErr error
		for _, b := range bindings {
			db, err := dbmig.OpenInstance(ctx, inst, secret, b.DbName)
			if err != nil {
				allExist = false
				lastErr = err
				continue
			}
			exists, _ := PublicationExists(ctx, db, b.PublicationName)
			names, _ := ListPublicationTables(ctx, db, b.PublicationName)
			if !exists {
				allExist = false
			}
			totalTables += len(names)
			if slotInfo == nil && strings.TrimSpace(b.SlotName) != "" {
				if info, serr := GetReplicationSlot(ctx, db, b.SlotName); serr == nil {
					slotInfo = info
				}
			}
			db.Close()
			checked++
		}
		if checked == 0 {
			msg := "connect source pg failed"
			if lastErr != nil {
				msg = lastErr.Error()
			}
			pg.State = "UNREACHABLE"
			pg.Message = msg
			publication.Unreachable = true
			publication.Message = msg
		} else {
			pg.State = "READY"
			publication.Exists = allExist
			publication.TableCount = totalTables
			if !allExist {
				publication.Message = "部分分库 Publication 不存在"
			}
		}
	} else {
		pg.State = "UNREACHABLE"
		pg.Message = "源端实例不存在"
		publication.Unreachable = true
		publication.Message = pg.Message
	}

	connStates := make([]map[string]any, 0)
	worst := "UNKNOWN"
	if o != nil {
		names := o.taskConnectorNames(t, extra)
		for _, name := range names {
			stMap, err := o.ConnectorStatus(ctx, extra, name)
			if stMap == nil {
				stMap = map[string]any{}
			}
			state := connectorStateOf(stMap)
			if ConnectorStatusNotFound(stMap) {
				state = PresenceNotFound
			} else if err != nil {
				state = "UNKNOWN"
				stMap["error"] = err.Error()
			}
			stMap["name"] = name
			stMap["state"] = state
			connStates = append(connStates, stMap)
			if worst == "UNKNOWN" || !strings.EqualFold(state, "RUNNING") {
				worst = state
				if !strings.EqualFold(state, "RUNNING") && state != "" && state != "UNKNOWN" {
					break
				}
			}
		}
	}
	var primaryConn map[string]any
	if len(connStates) > 0 {
		primaryConn = connStates[0]
		for _, c := range connStates {
			if !strings.EqualFold(fmt.Sprint(c["state"]), "RUNNING") {
				primaryConn = c
				break
			}
		}
		if worst == "UNKNOWN" {
			worst = connectorStateOf(primaryConn)
		}
	}

	brokers := ""
	if o != nil {
		brokers = o.resolveKafkaBrokers(extra)
	} else {
		brokers = extra["kafka_bootstrap_servers"]
	}
	topics := CollectPipelineTopicNames(prefix, schema, maps, tables)
	kafka := &PipelineKafka{State: "UNKNOWN", Brokers: brokers}
	var kafkaTopics []PipelineKafkaTopic
	if brokers == "" {
		kafka.State = "SKIPPED"
		kafka.Message = "Kafka 未配置"
	} else {
		exist, kerr := ProbeKafkaTopics(ctx, brokers, topics)
		if kerr != nil {
			kafka.State = "UNREACHABLE"
			kafka.Message = kerr.Error()
		} else {
			missing := 0
			for _, name := range topics {
				ok := exist[name]
				kafkaTopics = append(kafkaTopics, PipelineKafkaTopic{Name: name, Exists: ok})
				if !ok {
					missing++
				}
			}
			if missing > 0 {
				kafka.State = "MISSING"
				kafka.Message = fmt.Sprintf("%d/%d topic 不存在", missing, len(topics))
			} else {
				kafka.State = "READY"
			}
		}
	}
	if kafkaTopics == nil {
		kafkaTopics = []PipelineKafkaTopic{}
	}

	sr := &PipelineStarRocks{State: "SKIPPED", Message: "未配置目标端"}
	rls := []PipelineRoutineLoad{}
	if extra["auto_create_table"] == "false" || strings.TrimSpace(t.TargetID) == "" {
		sr.State = "SKIPPED"
		sr.Message = "手动 DDL 或未配置目标端"
	} else if inst, _ := st.GetInstance(t.TargetID); inst == nil {
		sr.State = "UNREACHABLE"
		sr.Message = "目标实例不存在"
	} else if db, err := dbmig.OpenInstance(ctx, inst, secret, t.TargetDB); err != nil {
		sr.State = "UNREACHABLE"
		sr.Message = err.Error()
	} else {
		targets := collectTargetTables(maps, tables)
		rls = ListRoutineLoadStates(ctx, db, t.TargetDB, targets, extra["env"], t.SourceDB, prefix)
		db.Close()
		sr.State = "READY"
		bad := 0
		for _, rl := range rls {
			if !routineLoadHealthy(rl.State) {
				bad++
			}
		}
		if bad > 0 {
			sr.State = "DEGRADED"
			sr.Message = fmt.Sprintf("%d 个 Routine Load 异常", bad)
		}
	}

	// healthy 对齐老项目列表口径：Kafka MISSING/PARTIAL 不判全局不健康；手动 DDL 豁免 SR/RL。
	manualDDL := extra["auto_create_table"] == "false"
	connectorOK := strings.EqualFold(worst, "RUNNING") ||
		worst == "" ||
		(strings.EqualFold(worst, "UNKNOWN") && extra["connector_name"] == "")
	kafkaOK := kafka.State == "READY" || kafka.State == "SKIPPED" ||
		kafka.State == "MISSING" || kafka.State == "PARTIAL" || kafka.State == "UNKNOWN"
	srOK := manualDDL || sr.State == "READY" || sr.State == "SKIPPED"
	healthy := strings.EqualFold(pg.State, "READY") && publication.Exists && connectorOK && kafkaOK && srOK
	reason := ""
	if !healthy {
		switch {
		case pg.State == "UNREACHABLE":
			reason = "源端 PG 不可达"
		case !publication.Exists:
			reason = "Publication 不存在"
		case worst == PresenceNotFound:
			reason = "Debezium Connector 缺失，需重新初始化"
		case strings.EqualFold(worst, "FAILED") || strings.EqualFold(worst, "PAUSED"):
			reason = "Debezium Connector " + worst
		case kafka.State == "UNREACHABLE":
			reason = kafka.Message
		case !manualDDL && (sr.State == "UNREACHABLE" || sr.State == "DEGRADED"):
			reason = sr.Message
		default:
			reason = "链路未完全就绪"
		}
	}

	existN := 0
	for _, kt := range kafkaTopics {
		if kt.Exists {
			existN++
		}
	}
	kafka.TopicCount = len(kafkaTopics)
	kafka.AllExist = len(kafkaTopics) > 0 && existN == len(kafkaTopics)

	pgSource := map[string]any{"state": pg.State, "message": pg.Message, "host": pgHost, "port": pgPort}
	if primaryConn != nil {
		primaryConn["debezium_mode"] = extra["debezium_mode"]
	}
	slotOut := slotInfo
	if slotOut == nil && slot != "" {
		slotOut = &PipelineSlot{Name: slot}
	}

	steps := []PipelineNode{
		{ID: "pg", Name: "PG Source", Detail: hostPort(pgHost, pgPort), State: pg.State},
		{ID: "publication", Name: "Publication", Detail: pub, State: pubState(publication)},
		{ID: "slot", Name: "Replication Slot", Detail: slot, State: slotState(slotOut)},
		{ID: "debezium", Name: "Debezium Connector", Detail: connector, State: worst},
		{ID: "kafka", Name: "Kafka Topics", Detail: prefix, State: kafka.State},
		{ID: "starrocks", Name: "StarRocks", Detail: t.TargetDB, State: sr.State},
		{ID: "routine_load", Name: "Routine Load", Detail: fmt.Sprintf("%d", len(rls)), State: sr.State},
	}

	pipe := map[string]any{
		"pg": pg, "pg_source": pgSource, "publication": publication, "slot": slotOut,
		"connector": primaryConn, "kafka": kafka, "kafka_topics": kafkaTopics,
		"starrocks": sr, "routine_loads": rls, "connectors": connStates,
	}

	out := map[string]any{
		"task_id": t.ID, "status": t.Status, "pipeline_status": extra["pipeline_status"],
		"error_step": extra["error_step"], "error_message": extra["error_message"],
		"publication_name": pub, "slot_name": slot,
		"topic_prefix": prefix, "steps": steps, "shard_db": extra["shard_db"] == "true",
		"source_db_names": extra["source_db_names"],
		"pg":              pg, "pg_source": pgSource, "publication": publication, "kafka": kafka, "kafka_topics": kafkaTopics,
		"starrocks": sr, "routine_loads": rls, "connectors": connStates,
		"pipeline": pipe, "healthy": healthy, "unhealthy_reason": reason,
	}
	if slotInfo != nil {
		out["slot"] = slotInfo
	}
	if primaryConn != nil {
		out["connector"] = primaryConn
	}
	return out
}

// ConnectorStatusNotFound Connect GET /connectors/{name}/status 明确返回 404。
func ConnectorStatusNotFound(st map[string]any) bool {
	if st == nil {
		return false
	}
	switch v := st["http_status"].(type) {
	case int:
		return v == 404
	case float64:
		return v == 404
	}
	return false
}

func connectorStateOf(st map[string]any) string {
	if st == nil {
		return "UNKNOWN"
	}
	if c, ok := st["connector"].(map[string]any); ok {
		if v, ok := c["state"].(string); ok && v != "" {
			return v
		}
	}
	if v, ok := st["state"].(string); ok && v != "" {
		return v
	}
	return "UNKNOWN"
}

func collectTargetTables(maps []TableMapping, fallback []string) []string {
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
	for _, m := range maps {
		if strings.EqualFold(m.Status, "deleted") {
			continue
		}
		add(m.TargetTable)
	}
	if len(out) == 0 {
		for _, t := range fallback {
			add(t)
		}
	}
	return out
}

func routineLoadHealthy(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "RUNNING", "NEED_SCHEDULE", "PAUSED", "":
		return true
	default:
		return false
	}
}

func PublicationExists(ctx context.Context, db *sql.DB, pub string) (bool, error) {
	pub = strings.TrimSpace(pub)
	if db == nil || pub == "" {
		return false, nil
	}
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM pg_publication WHERE pubname=$1`, pub).Scan(&n)
	return n > 0, err
}

func GetReplicationSlot(ctx context.Context, db *sql.DB, slot string) (*PipelineSlot, error) {
	slot = strings.TrimSpace(slot)
	if db == nil || slot == "" {
		return nil, fmt.Errorf("slot empty")
	}
	info := &PipelineSlot{Name: slot}
	var active sql.NullBool
	var restart sql.NullString
	var lag sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT slot_name, active, restart_lsn::text,
		       COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn), 0)
		FROM pg_replication_slots WHERE slot_name=$1`, slot).Scan(&info.Name, &active, &restart, &lag)
	if err != nil {
		info.Message = err.Error()
		return info, err
	}
	info.Active = active.Bool
	if restart.Valid {
		info.RestartLSN = restart.String
	}
	info.LagBytes = lag.Int64
	info.ReplicationLagBytes = lag.Int64
	return info, nil
}

func hostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", host, port)
	}
	return host
}

func pubState(p *PipelinePublication) string {
	if p == nil {
		return "UNKNOWN"
	}
	if p.Unreachable {
		return "UNREACHABLE"
	}
	if p.Exists {
		return "READY"
	}
	if p.Name == "" {
		return "UNKNOWN"
	}
	return "MISSING"
}

func slotState(s *PipelineSlot) string {
	if s == nil || s.Name == "" {
		return "UNKNOWN"
	}
	if s.Message != "" && !s.Active {
		return "UNREACHABLE"
	}
	if s.Active {
		return "READY"
	}
	return "PAUSED"
}

func ListRoutineLoadStates(ctx context.Context, db *sql.DB, targetDB string, tables []string, env, sourceDB, topicPrefix string) []PipelineRoutineLoad {
	out := make([]PipelineRoutineLoad, 0, len(tables))
	if db == nil {
		return out
	}
	for _, table := range tables {
		name := RoutineLoadNameFor(env, sourceDB, topicPrefix, table)
		item := PipelineRoutineLoad{Name: name, Table: table, State: "UNKNOWN"}
		rows, err := db.QueryContext(ctx, fmt.Sprintf("SHOW ROUTINE LOAD FOR `%s`.`%s`", targetDB, name))
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "not exist") || strings.Contains(low, "unknown") {
				item.State = "NOT_FOUND"
			} else {
				item.Reason = err.Error()
			}
			out = append(out, item)
			continue
		}
		cols, _ := rows.Columns()
		stateIdx := indexFold(cols, "State")
		reasonIdx := indexFold(cols, "ReasonOfStateChanged")
		if stateIdx < 0 {
			stateIdx = indexFold(cols, "STATE")
		}
		found := false
		for rows.Next() {
			found = true
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if rows.Scan(ptrs...) != nil {
				continue
			}
			if stateIdx >= 0 {
				item.State = fmt.Sprint(asString(vals[stateIdx]))
			}
			if reasonIdx >= 0 {
				item.Reason = asString(vals[reasonIdx])
			}
		}
		rows.Close()
		if !found {
			item.State = "NOT_FOUND"
		}
		out = append(out, item)
	}
	return out
}

func indexFold(cols []string, name string) int {
	for i, c := range cols {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}
