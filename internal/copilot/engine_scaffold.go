package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"auto-dba/internal/aiops"
	"auto-dba/internal/dbprobe"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

var reDBIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// 各引擎只读语句。模型只能选名字，不能提交 SQL。
var (
	pgScaffoldSQL = map[string]string{
		"activity":    `SELECT pid, usename, datname, state, wait_event_type, left(query, 200) AS query FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND state IS NOT NULL LIMIT 50`,
		"connections": `SELECT datname, count(*) AS sessions FROM pg_stat_activity GROUP BY datname ORDER BY sessions DESC LIMIT 50`,
		"databases":   `SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname LIMIT 50`,
		"sizes":       `SELECT datname, pg_database_size(datname) AS bytes FROM pg_database WHERE datistemplate = false ORDER BY bytes DESC LIMIT 50`,
	}
	chScaffoldSQL = map[string]string{
		"processes": `SELECT user, query_id, elapsed, read_rows, substring(query, 1, 200) AS query FROM system.processes LIMIT 50`,
		"queries":   `SELECT user, type, query_duration_ms, read_rows, substring(query, 1, 200) AS query FROM system.query_log WHERE event_time > now() - INTERVAL 1 HOUR ORDER BY query_duration_ms DESC LIMIT 50`,
		"databases": `SELECT name, engine FROM system.databases ORDER BY name LIMIT 50`,
	}
)

const (
	redisCPUExpr  = `sum by (instance) (rate(redis_cpu_user_seconds_total{__SEL__}[5m]) + rate(redis_cpu_sys_seconds_total{__SEL__}[5m]))`
	redisMemExpr  = `max by (instance) (redis_memory_used_bytes{__SEL__})`
	redisConnExpr = `max by (instance) (redis_connected_clients{__SEL__})`
	chCPUExpr     = `1 - avg by (instance) ({__name__=~"ClickHouseAsyncMetrics_OSIdleTimeCPU.*",__SEL__})`
	pgConnExpr    = `sum(pg_stat_activity_count{__SEL__})`
	srMemExpr     = `starrocks_be_process_mem_bytes`
)

// scaffoldCall 一条只读取数。Kind 是脚手架名，Spec 是 SQL 名字或指标表达式。
type scaffoldCall struct {
	Kind  string
	Spec  string
	Hours int
}

// chooseScaffolds 按平台实例引擎和问题内容排出一组脚手架。
// 连接、慢查询这类问题会同时要实例内查询和 Prometheus，单路结果不够分析。
func chooseScaffolds(message string, inst *store.Instance) []scaffoldCall {
	if inst == nil {
		return nil
	}
	eng := scaffoldEngine(inst.DBType)
	if eng == "" {
		eng = aiops.ParseUtterance(message).Engine
	}
	low := strings.ToLower(message)
	hours := utteranceHours(aiops.ParseUtterance(message).Time)
	var out []scaffoldCall
	add := func(kind, spec string, h int) {
		out = append(out, scaffoldCall{Kind: kind, Spec: spec, Hours: h})
	}
	switch eng {
	case "postgres":
		if wantsDisk(message) {
			add("postgres", "sizes", 0)
		}
		if strings.Contains(message, "连接") || strings.Contains(message, "会话") || strings.Contains(message, "活动") || strings.Contains(message, "锁") {
			add("postgres", "connections", 0)
			add("postgres", "activity", 0)
			add("prometheus", pgConnExpr, hours)
		}
	case "clickhouse":
		switch {
		case strings.Contains(message, "慢") || strings.Contains(message, "查询"):
			add("clickhouse", "queries", 0)
			add("prometheus", chCPUExpr, hours)
		case strings.Contains(message, "进程") || strings.Contains(message, "正在"):
			add("clickhouse", "processes", 0)
			add("prometheus", chCPUExpr, hours)
		case strings.Contains(low, "cpu"):
			add("prometheus", chCPUExpr, hours)
		case strings.Contains(message, "库"):
			add("clickhouse", "databases", 0)
		}
	case "starrocks":
		if strings.Contains(low, "cpu") {
			add("prometheus", beCPUExpr, hours)
		}
		if strings.Contains(message, "内存") || strings.Contains(low, "mem") {
			add("prometheus", srMemExpr, hours)
		}
	case "redis":
		if strings.Contains(low, "cpu") {
			add("prometheus", redisCPUExpr, hours)
		}
		if strings.Contains(message, "内存") || strings.Contains(low, "mem") {
			add("prometheus", redisMemExpr, hours)
		}
		if strings.Contains(message, "连接") {
			add("prometheus", redisConnExpr, hours)
		}
	}
	return out
}

func wantsDisk(message string) bool {
	return strings.Contains(message, "磁盘") || strings.Contains(message, "空间") || strings.Contains(message, "容量")
}

func scaffoldEngine(dbType string) string {
	got := store.NormalizeEngineType(dbType)
	switch got {
	case "starrocks", "doris", "sr":
		return "starrocks"
	case "postgres", "clickhouse", "redis", "mysql":
		return got
	default:
		return got
	}
}

func scaffoldSQL(kind, name string) (dbName, sqlText string, err error) {
	name = strings.ToLower(strings.TrimSpace(name))
	var src map[string]string
	switch kind {
	case "postgres":
		src = pgScaffoldSQL
		dbName = "postgres"
	case "clickhouse":
		src = chScaffoldSQL
		dbName = "default"
	default:
		return "", "", fmt.Errorf("没有 %s 查询脚手架", kind)
	}
	sqlText = src[name]
	if sqlText == "" {
		return "", "", fmt.Errorf("%s 脚手架没有查询 %s", kind, name)
	}
	if _, err = CheckCopilotSQL(sqlText); err != nil {
		return "", "", err
	}
	return dbName, sqlText, nil
}

func (s *Service) runScaffold(ctx context.Context, tenant string, inst *store.Instance, kind, spec string, hours int) (TaskStep, string) {
	switch kind {
	case "prometheus":
		return s.promStep(ctx, inst, spec, hours)
	case "postgres", "clickhouse":
		return s.sqlScaffoldStep(ctx, tenant, inst, kind, spec)
	default:
		return TaskStep{Action: kind, Status: "error", Output: "未知脚手架"}, ""
	}
}

// prepareSelectSQL 校验模型写的单条查询，并确定要连接的库。缺库名或库名非法时不退回维护库。
func prepareSelectSQL(database, rawSQL string) (dbName, sqlText string, err error) {
	dbName = strings.TrimSpace(database)
	if !reDBIdent.MatchString(dbName) {
		return "", "", fmt.Errorf("查询 SQL 必须指定合法库名，未连接实例")
	}
	sqlText, err = CheckCopilotSQL(rawSQL)
	if err != nil {
		return "", "", err
	}
	return dbName, sqlText, nil
}

func (s *Service) sqlSelectStep(ctx context.Context, tenant string, inst *store.Instance, database, rawSQL string) (TaskStep, string) {
	detail := strings.TrimSpace(database)
	if inst == nil {
		return TaskStep{Action: "postgres", Status: "error", Detail: detail, Output: "未对上平台实例，未查询。"}, ""
	}
	if scaffoldEngine(inst.DBType) != "postgres" {
		return TaskStep{
			Action: "postgres", Status: "error", Detail: detail,
			Output: fmt.Sprintf("平台实例引擎是 %s，不使用 postgres 查询。", inst.DBType),
		}, ""
	}
	dbName, sqlText, err := prepareSelectSQL(database, rawSQL)
	if err != nil {
		return TaskStep{Action: "postgres", Status: "error", Detail: detail, Output: err.Error()}, ""
	}
	body, err := s.execReadOnlySQL(ctx, tenant, inst, dbName, sqlText)
	if err != nil {
		return TaskStep{Action: "postgres", Status: "error", Detail: dbName, Output: err.Error()}, ""
	}
	return TaskStep{Action: "postgres", Status: "ok", Detail: dbName, Output: trim(body, 1500)}, body
}

func (s *Service) sqlScaffoldStep(ctx context.Context, tenant string, inst *store.Instance, kind, name string) (TaskStep, string) {
	if inst == nil {
		return TaskStep{Action: kind, Status: "error", Detail: name, Output: "未对上平台实例，未查询。"}, ""
	}
	if scaffoldEngine(inst.DBType) != kind {
		return TaskStep{
			Action: kind, Status: "error", Detail: name,
			Output: fmt.Sprintf("平台实例引擎是 %s，不使用 %s 查询脚手架。", inst.DBType, kind),
		}, ""
	}
	dbName, sqlText, err := scaffoldSQL(kind, name)
	if err != nil {
		return TaskStep{Action: kind, Status: "error", Detail: name, Output: err.Error()}, ""
	}
	body, err := s.execReadOnlySQL(ctx, tenant, inst, dbName, sqlText)
	if err != nil {
		return TaskStep{Action: kind, Status: "error", Detail: name, Output: err.Error()}, ""
	}
	out := trim(body, 1500)
	if table, _, _, _, ok := formatSizeTable(body); ok {
		out = table
	}
	return TaskStep{Action: kind, Status: "ok", Detail: name, Output: out}, body
}

func (s *Service) execReadOnlySQL(ctx context.Context, tenant string, inst *store.Instance, dbName, sqlText string) (string, error) {
	sqlText, err := CheckCopilotSQL(sqlText)
	if err != nil {
		return "", err
	}
	if s == nil || s.Store == nil || inst == nil {
		return "", fmt.Errorf("无法执行实例查询")
	}
	if tenant != "" && inst.TenantID != "" && inst.TenantID != tenant {
		return "", fmt.Errorf("实例不属于当前租户")
	}
	if strings.TrimSpace(s.Secret) == "" || strings.TrimSpace(inst.Password) == "" {
		return "", fmt.Errorf("实例密码不可用，已拒绝执行")
	}
	pass, err := secret.Decrypt(s.Secret, inst.Password)
	if err != nil {
		return "", fmt.Errorf("解密实例密码失败")
	}
	limit := maxTableRows
	qctx, cancel := context.WithTimeout(dbprobe.WithReadOnlySession(ctx), sqlTimeout)
	defer cancel()
	res, err := dbprobe.ExecQuery(qctx, inst, pass, dbName, sqlText, limit)
	if err != nil {
		return "", err
	}
	if len(res.Rows) > maxTableRows {
		res.Rows = res.Rows[:maxTableRows]
	}
	raw, _ := json.Marshal(map[string]any{"columns": res.Columns, "rows": res.Rows})
	return Redact(string(raw)), nil
}
