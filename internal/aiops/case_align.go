package aiops

import (
	"strings"

	"auto-dba/internal/store"
)

// CaseHint 与告警对齐的 SQL 洞察案例摘要（供 Bundle / Webhook）。
type CaseHint struct {
	Fingerprint       string `json:"fingerprint"`
	MetricKey         string `json:"metric_key"`
	Severity          string `json:"severity"`
	Title             string `json:"title"`
	RootCauseTemplate string `json:"root_cause_template,omitempty"`
	ActionTemplate    string `json:"action_template,omitempty"`
	ConfirmCount      int    `json:"confirm_count"`
	HitCount          int    `json:"hit_count"`
	MatchReason       string `json:"match_reason"`
}

// DefaultAlertNameMetrics 种子告警 alertname → SQL 洞察 metric_key。
// 与 store.DefaultAlertRules / 知识库对齐；额外条目来自 PostgresKnowledgeCases。
var DefaultAlertNameMetrics = map[string][]string{
	"HostDisk":                {"disk_usage"},
	"HostCPUHigh":             {"cpu_usage"},
	"MySQLConn":               {"conn_usage_ratio"},
	"MySQLThreadsRunningHigh": {"backends", "lock_waiters", "max_xact_duration"},
	"MySQLReplicationLag":     {"replication_lag", "binlog_disk"},
	"MySQLDeadlocks":          {"deadlocks_ps", "lock_waiters"},
	"MySQLLockWait":           {"lock_waiters"},
	"MySQLSlowQuery":          {"slow_query_cnt", "tmp_disk_tables"},
	"MySQLBufferPoolLow":      {"cache_hit_ratio"},
	"MySQLDisk":               {"disk_usage", "binlog_disk"},
	"MySQLLongTransaction":    {"max_xact_duration"},
	"MySQLHA":                 {"mysql_ha", "replication_lag"},
	"PGConnHigh":              {"conn_usage_ratio", "backends", "idle_in_transaction"},
	"PGStorage":               {"disk_usage", "wal_retain_bytes", "dead_tup_ratio", "archive_lag"},
	"PGCacheHitLow":           {"cache_hit_ratio"},
	"PGReplicationLag":        {"replication_lag", "wal_retain_bytes"},
	"PGLongTransaction":       {"max_xact_duration", "idle_in_transaction", "xid_age"},
	"PGDeadlocks":             {"deadlocks_ps", "lock_waiters"},
	"PGSlowQuery":             {"slow_query_cnt", "invalid_index", "seq_scan_ratio"},
	"PGTPSDrop":               {"tps"},
	"PGActiveSessions":        {"active_session_count", "lock_waiters"},
	"RedisMemHigh":            {"mem_usage", "bigkey_count"},
	"RedisBigKey":             {"bigkey_count"},
	"RedisHotKey":             {"hotkey_qps"},
	"RedisSlowCommand":        {"slow_query_cnt", "bigkey_count"},
	"RedisReplicationLag":     {"replication_lag"},
	"RedisFailover":           {"failover_events"},
	"RedisCPUHigh":            {"cpu_usage", "hotkey_qps"},
	"RedisConnHigh":           {"conn_usage_ratio"},
	"RedisPersistence":        {"rdb_cow_mem", "mem_usage"},
	"SRBEDown":                {"be_alive_ratio"},
	"SRTabletUnhealthy":       {"unhealthy_tablet"},
	"SRLoadFail":              {"load_error_rate", "load_timeout"},
	"SRQueryTimeout":          {"query_timeout_cnt", "fe_gc_pause"},
	"SRBEMemHigh":             {"be_mem_usage"},
	"SRFEConnHigh":            {"conn_usage_ratio"},
	"SRFEGC":                  {"fe_gc_pause"},
	"SRCompactionLag":         {"compaction_score"},
}

// MetricAlertNames 反向：metric_key → 建议挂载的 alertname（用于种子案例）。
var MetricAlertNames = map[string][]string{
	"disk_usage":            {"HostDisk", "PGStorage", "MySQLDisk"},
	"cpu_usage":             {"HostCPUHigh", "RedisCPUHigh"},
	"conn_usage_ratio":      {"MySQLConn", "PGConnHigh", "RedisConnHigh", "SRFEConnHigh"},
	"backends":              {"MySQLThreadsRunningHigh", "PGConnHigh"},
	"mem_usage":             {"RedisMemHigh"},
	"cache_hit_ratio":       {"PGCacheHitLow", "MySQLBufferPoolLow"},
	"replication_lag":       {"PGReplicationLag", "MySQLReplicationLag", "RedisReplicationLag"},
	"deadlocks_ps":          {"PGDeadlocks", "MySQLDeadlocks"},
	"slow_query_cnt":        {"PGSlowQuery", "MySQLSlowQuery", "RedisSlowCommand"},
	"tps":                   {"PGTPSDrop"},
	"max_xact_duration":     {"PGLongTransaction", "MySQLLongTransaction"},
	"active_session_count":  {"PGActiveSessions"},
	"wal_retain_bytes":      {"PGStorage", "PGReplicationLag"},
	"dead_tup_ratio":        {"PGStorage", "PGLongTransaction"},
	"autovacuum_lag":        {"PGStorage", "PGLongTransaction"},
	"xid_age":               {"PGLongTransaction"},
	"lock_waiters":          {"PGActiveSessions", "PGDeadlocks", "MySQLLockWait", "MySQLThreadsRunningHigh"},
	"idle_in_transaction":   {"PGLongTransaction", "PGConnHigh"},
	"invalid_index":         {"PGSlowQuery"},
	"archive_lag":           {"PGStorage"},
	"seq_scan_ratio":        {"PGSlowQuery"},
	"checkpoint_io":         {"HostCPUHigh", "PGStorage"},
	"backup_status":         {"PGStorage"},
	"oncall_triage":         {"PGActiveSessions", "PGSlowQuery", "PGStorage", "MySQLConn", "RedisMemHigh", "SRBEDown"},
	"binlog_disk":           {"MySQLDisk", "MySQLReplicationLag"},
	"tmp_disk_tables":       {"MySQLSlowQuery"},
	"bigkey_count":          {"RedisBigKey", "RedisSlowCommand", "RedisMemHigh"},
	"hotkey_qps":            {"RedisHotKey", "RedisCPUHigh"},
	"failover_events":       {"RedisFailover"},
	"rdb_cow_mem":           {"RedisPersistence", "RedisMemHigh"},
	"be_alive_ratio":        {"SRBEDown"},
	"unhealthy_tablet":      {"SRTabletUnhealthy"},
	"load_error_rate":       {"SRLoadFail"},
	"load_timeout":          {"SRLoadFail", "SRQueryTimeout"},
	"query_timeout_cnt":     {"SRQueryTimeout"},
	"be_mem_usage":          {"SRBEMemHigh"},
	"fe_gc_pause":           {"SRFEGC", "SRQueryTimeout"},
	"compaction_score":      {"SRCompactionLag"},
	"mysql_ha":              {"MySQLHA", "MySQLReplicationLag"},
}

// AlertNameFromLabels 读取 alertname（规则 labels 合并进告警）。
func AlertNameFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return strings.TrimSpace(labels["alertname"])
}

// MetricKeysForAlert 根据 alertname / 注解 / 标题启发式得到候选 metric_key。
func MetricKeysForAlert(a *store.Alert, rule *store.AlertRule) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(k string) {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	if a != nil {
		if mk := firstLabel(a.Labels, "metric_key", "si_metric"); mk != "" {
			add(mk)
		}
		if a.Annotations != nil {
			add(a.Annotations["metric_key"])
			add(a.Annotations["si_metric"])
		}
		if an := AlertNameFromLabels(a.Labels); an != "" {
			for _, mk := range DefaultAlertNameMetrics[an] {
				add(mk)
			}
		}
		// 标题/服务启发式（无 alertname 时）
		hay := strings.ToLower(a.Title + " " + a.Service)
		switch {
		case strings.Contains(hay, "磁盘") || strings.Contains(hay, "disk") || strings.Contains(hay, "storage"):
			add("disk_usage")
		case strings.Contains(hay, "cpu"):
			add("cpu_usage")
		case strings.Contains(hay, "连接") || strings.Contains(hay, "conn"):
			add("conn_usage_ratio")
			add("backends")
		case strings.Contains(hay, "缓存") || strings.Contains(hay, "缓冲") || strings.Contains(hay, "cache"):
			add("cache_hit_ratio")
		case strings.Contains(hay, "复制") || strings.Contains(hay, "lag"):
			add("replication_lag")
		case strings.Contains(hay, "死锁") || strings.Contains(hay, "deadlock"):
			add("deadlocks_ps")
		case strings.Contains(hay, "tps"):
			add("tps")
		case strings.Contains(hay, "长事务") || strings.Contains(hay, "xact"):
			add("max_xact_duration")
		case strings.Contains(hay, "慢查询") || strings.Contains(hay, "slow"):
			add("slow_query_cnt")
		case strings.Contains(hay, "wal") || strings.Contains(hay, "复制槽") || strings.Contains(hay, "replication slot"):
			add("wal_retain_bytes")
			add("disk_usage")
		case strings.Contains(hay, "膨胀") || strings.Contains(hay, "bloat") || strings.Contains(hay, "死元组"):
			add("dead_tup_ratio")
			add("disk_usage")
		case strings.Contains(hay, "vacuum") || strings.Contains(hay, "autovacuum"):
			add("autovacuum_lag")
			add("dead_tup_ratio")
		case strings.Contains(hay, "xid") || strings.Contains(hay, "wraparound") || strings.Contains(hay, "冻结"):
			add("xid_age")
		case strings.Contains(hay, "锁等待") || strings.Contains(hay, "阻塞") || strings.Contains(hay, "lock wait"):
			add("lock_waiters")
			add("active_session_count")
		case strings.Contains(hay, "idle in transaction") || strings.Contains(hay, "空闲事务"):
			add("idle_in_transaction")
			add("max_xact_duration")
		case strings.Contains(hay, "无效索引") || strings.Contains(hay, "invalid index"):
			add("invalid_index")
		case strings.Contains(hay, "归档") || strings.Contains(hay, "archive"):
			add("archive_lag")
			add("disk_usage")
		case strings.Contains(hay, "顺序扫") || strings.Contains(hay, "seq scan"):
			add("seq_scan_ratio")
			add("slow_query_cnt")
		case strings.Contains(hay, "大key") || strings.Contains(hay, "bigkey"):
			add("bigkey_count")
			add("mem_usage")
		case strings.Contains(hay, "热key") || strings.Contains(hay, "hotkey"):
			add("hotkey_qps")
		case strings.Contains(hay, "binlog"):
			add("binlog_disk")
			add("disk_usage")
		case strings.Contains(hay, "starrocks") || strings.Contains(hay, "be 宕") || strings.Contains(hay, "tablet"):
			add("be_alive_ratio")
			add("unhealthy_tablet")
		case strings.Contains(hay, "导入失败") || strings.Contains(hay, "broker load"):
			add("load_error_rate")
		}
	}
	if rule != nil && rule.Labels != nil {
		if an := strings.TrimSpace(rule.Labels["alertname"]); an != "" {
			for _, mk := range DefaultAlertNameMetrics[an] {
				add(mk)
			}
		}
	}
	return out
}

// MatchCasesForAlert 将告警对齐到 SqlInsightCase（fingerprint / alert_names / metric_key）。
func MatchCasesForAlert(cases []*store.SqlInsightCaseRow, a *store.Alert, rule *store.AlertRule) []CaseHint {
	if len(cases) == 0 || a == nil {
		return nil
	}
	alertName := AlertNameFromLabels(a.Labels)
	if alertName == "" && rule != nil && rule.Labels != nil {
		alertName = strings.TrimSpace(rule.Labels["alertname"])
	}
	metrics := MetricKeysForAlert(a, rule)
	metricSet := map[string]struct{}{}
	for _, m := range metrics {
		metricSet[m] = struct{}{}
	}

	var out []CaseHint
	seen := map[string]struct{}{}
	add := func(c *store.SqlInsightCaseRow, reason string) {
		if c == nil || c.Fingerprint == "" {
			return
		}
		if _, ok := seen[c.Fingerprint]; ok {
			return
		}
		seen[c.Fingerprint] = struct{}{}
		out = append(out, CaseHint{
			Fingerprint: c.Fingerprint, MetricKey: c.MetricKey, Severity: c.Severity,
			Title: c.Title, RootCauseTemplate: c.RootCauseTemplate, ActionTemplate: c.ActionTemplate,
			ConfirmCount: c.ConfirmCount, HitCount: c.HitCount, MatchReason: reason,
		})
	}

	// 1) 注解显式指纹
	if a.Annotations != nil {
		if fp := strings.TrimSpace(a.Annotations["si_fingerprint"]); fp != "" {
			for _, c := range cases {
				if c.Fingerprint == fp {
					add(c, "annotation:si_fingerprint")
				}
			}
		}
	}
	// 2) case.alert_names 含 alertname
	if alertName != "" {
		anLower := strings.ToLower(alertName)
		for _, c := range cases {
			for _, n := range ParseAlertNames(c.AlertNames) {
				if strings.ToLower(n) == anLower {
					add(c, "alert_names:"+alertName)
					break
				}
			}
		}
	}
	// 3) metric_key 对齐
	for _, c := range cases {
		if _, ok := metricSet[strings.ToLower(strings.TrimSpace(c.MetricKey))]; ok {
			add(c, "metric_key:"+c.MetricKey)
		}
	}
	return out
}

// ParseAlertNames 逗号 / 空格分隔的 alertname 列表。
func ParseAlertNames(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == ' '
	})
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// JoinAlertNames 规范化写入。
func JoinAlertNames(names []string) string {
	seen := map[string]struct{}{}
	var out []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		k := strings.ToLower(n)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	return strings.Join(out, ",")
}

// SuggestedAlertNamesForMetric 种子案例用（静态表 + 知识库合并）。
func SuggestedAlertNamesForMetric(metricKey string) string {
	key := strings.ToLower(strings.TrimSpace(metricKey))
	names := append([]string{}, MetricAlertNames[key]...)
	for _, kc := range AllKnowledgeCases() {
		if strings.ToLower(kc.MetricKey) != key {
			continue
		}
		names = append(names, kc.AlertNames...)
	}
	return JoinAlertNames(names)
}
