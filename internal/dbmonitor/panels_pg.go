package dbmonitor

import "strings"

const selToken = "__SEL__"

// PanelKind 面板类型。
const (
	KindStat       = "stat"
	KindTimeseries = "timeseries"
)

// 指标范围 / 数据源（对齐 AWR detectSource）。
const (
	ScopeDB   = "db"
	ScopeHost = "host"

	SourceQCE  = "tencent_qce"
	SourceSelf = "self_pg"
)

// PanelDef 面板定义。
//
// PromQL 对标 Grafana：
//   - 库指标：postgres_exporter（9628 风格 SUM / rate）
//   - 主机：仅腾讯云 qce_postgres_*（$idc + $db），不读 node_exporter
type PanelDef struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Unit       string   `json:"unit"`
	Section    string   `json:"section"`
	Kind       string   `json:"kind"`
	Scope      string   `json:"-"` // db | host
	PromQL     string   `json:"-"` // 默认（库 / 或主机 qce）
	PromQLQCE  string   `json:"-"` // 主机云监控（可与 PromQL 相同）
	LegendKeys []string `json:"legend_keys,omitempty"`
	Span       string   `json:"span,omitempty"` // half | full
}

func (p PanelDef) promQLFor(source string) string {
	if p.isHost() {
		if q := strings.TrimSpace(p.PromQLQCE); q != "" {
			return q
		}
		return p.PromQL
	}
	if source == SourceQCE && strings.TrimSpace(p.PromQLQCE) != "" {
		return p.PromQLQCE
	}
	return p.PromQL
}

func (p PanelDef) isHost() bool {
	return p.Scope == ScopeHost || isHostPanelKey(p.Key)
}

func isHostPanelKey(key string) bool {
	switch key {
	case "cpu_usage", "mem_usage", "disk_bytes", "disk_usage":
		return true
	default:
		return false
	}
}

// SectionDef 分区。
type SectionDef struct {
	Name   string     `json:"name"`
	Title  string     `json:"title"`
	Panels []PanelDef `json:"panels"`
}

func renderPromQL(tmpl, selector string) string {
	sel := strings.TrimSpace(selector)
	out := strings.ReplaceAll(tmpl, selToken, sel)
	out = strings.ReplaceAll(out, "{,", "{")
	out = strings.ReplaceAll(out, ",}", "}")
	out = strings.ReplaceAll(out, ",,", ",")
	return out
}

var pgSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "pg_version", Title: "Version", Unit: "text", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_static_version{__SEL__})`},
			{Key: "pg_start_time", Title: "Start Time", Unit: "unix", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_postmaster_start_time_seconds{__SEL__})`},
			{Key: "fetch_data", Title: "Current fetch data", Unit: "rows", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(pg_stat_database_tup_fetched{__SEL__})`},
			{Key: "insert_data", Title: "Current insert data", Unit: "rows", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(pg_stat_database_tup_inserted{__SEL__})`},
			{Key: "update_data", Title: "Current update data", Unit: "rows", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(pg_stat_database_tup_updated{__SEL__})`},
			{Key: "max_connections", Title: "Max Connections", Unit: "conns", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_max_connections{__SEL__})`},
			// 主机：仅 qce_postgres_*（对齐 Grafana，不读 node_exporter）
			{Key: "cpu_usage", Title: "CPU使用率", Unit: "ratio", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeHost, LegendKeys: []string{"d_b_instance_name"},
				PromQLQCE: `max by (d_b_instance_name) (qce_postgres_cpu_max{__SEL__}) / 100`,
				PromQL:    `max by (d_b_instance_name) (qce_postgres_cpu_max{__SEL__}) / 100`},
			{Key: "mem_usage", Title: "Memory使用率", Unit: "ratio", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeHost, LegendKeys: []string{"d_b_instance_name"},
				PromQLQCE: `max by (d_b_instance_name) (qce_postgres_memoryrate_max{__SEL__}) / 100`,
				PromQL:    `max by (d_b_instance_name) (qce_postgres_memoryrate_max{__SEL__}) / 100`},
			{Key: "disk_bytes", Title: "磁盘用量", Unit: "bytes", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeHost, LegendKeys: []string{"kind"},
				// 图例：数据/日志-idc-实例名，多 IDC 同名可区分
				PromQLQCE: `label_join(label_replace(max by (idc, d_b_instance_name) (qce_postgres_datafilesize_max{__SEL__}) * 1073741824, "kind", "数据", "", ""), "kind", "-", "kind", "idc", "d_b_instance_name") or label_join(label_replace(max by (idc, d_b_instance_name) (qce_postgres_logfilesize_max{__SEL__}) * 1073741824, "kind", "日志", "", ""), "kind", "-", "kind", "idc", "d_b_instance_name")`,
				PromQL:    `label_join(label_replace(max by (idc, d_b_instance_name) (qce_postgres_datafilesize_max{__SEL__}) * 1073741824, "kind", "数据", "", ""), "kind", "-", "kind", "idc", "d_b_instance_name") or label_join(label_replace(max by (idc, d_b_instance_name) (qce_postgres_logfilesize_max{__SEL__}) * 1073741824, "kind", "日志", "", ""), "kind", "-", "kind", "idc", "d_b_instance_name")`},
			{Key: "disk_usage", Title: "磁盘使用率", Unit: "ratio", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeHost, LegendKeys: []string{"d_b_instance_name"},
				PromQLQCE: `max by (d_b_instance_name) (qce_postgres_storagerate_max{__SEL__}) / 100`,
				PromQL:    `max by (d_b_instance_name) (qce_postgres_storagerate_max{__SEL__}) / 100`},
		},
	},
	{
		Name: "settings", Title: "Settings",
		Panels: []PanelDef{
			{Key: "shared_buffers", Title: "Shared Buffers", Unit: "bytes", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_shared_buffers_bytes{__SEL__}) or max(pg_settings_shared_buffers{__SEL__})`},
			{Key: "effective_cache", Title: "Effective Cache", Unit: "bytes", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_effective_cache_size_bytes{__SEL__}) or max(pg_settings_effective_cache_size{__SEL__})`},
			{Key: "maintenance_work_mem", Title: "Maintenance Work Mem", Unit: "bytes", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_maintenance_work_mem_bytes{__SEL__}) or max(pg_settings_maintenance_work_mem{__SEL__})`},
			{Key: "work_mem", Title: "Work Mem", Unit: "bytes", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_work_mem_bytes{__SEL__}) or max(pg_settings_work_mem{__SEL__})`},
			{Key: "max_wal_size", Title: "Max WAL Size", Unit: "bytes", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_max_wal_size_bytes{__SEL__}) or max(pg_settings_max_wal_size{__SEL__})`},
			{Key: "random_page_cost", Title: "Random Page Cost", Unit: "num", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_random_page_cost{__SEL__})`},
			{Key: "seq_page_cost", Title: "Seq Page Cost", Unit: "num", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_seq_page_cost{__SEL__})`},
			{Key: "max_worker_processes", Title: "Max Worker Processes", Unit: "num", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_max_worker_processes{__SEL__})`},
			{Key: "max_parallel_workers", Title: "Max Parallel Workers", Unit: "num", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(pg_settings_max_parallel_workers{__SEL__})`},
		},
	},
	{
		Name: "db_stats", Title: "Database Stats",
		Panels: []PanelDef{
			{Key: "fetched_ps", Title: "每秒fetched数量", Unit: "rows/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"datname"},
				PromQL: `topk(12, sum by (datname) (rate(pg_stat_database_tup_fetched{__SEL__,datname!~"template.*|postgres"}[5m])))`},
			{Key: "returned_ps", Title: "Number of rows returned", Unit: "rows/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"datname"},
				PromQL: `topk(12, sum by (datname) (rate(pg_stat_database_tup_returned{__SEL__,datname!~"template.*|postgres"}[5m])))`},
			{Key: "transactions", Title: "Transactions", Unit: "tx/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"datname"},
				PromQL: `topk(12, sum by (datname) (rate(pg_stat_database_xact_commit{__SEL__,datname!~"template.*|postgres"}[5m]) + rate(pg_stat_database_xact_rollback{__SEL__,datname!~"template.*|postgres"}[5m])))`},
			{Key: "cache_hit", Title: "Cache Hit Rate", Unit: "ratio", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"datname"},
				PromQL: `topk(12, sum by (datname) (rate(pg_stat_database_blks_hit{__SEL__,datname!~"template.*|postgres"}[5m])) / clamp_min(sum by (datname) (rate(pg_stat_database_blks_hit{__SEL__,datname!~"template.*|postgres"}[5m]) + rate(pg_stat_database_blks_read{__SEL__,datname!~"template.*|postgres"}[5m])), 1e-9))`},
		},
	},
	{
		Name: "sessions", Title: "Sessions / Locks / Temp",
		Panels: []PanelDef{
			{Key: "active_sessions", Title: "Active sessions", Unit: "conns", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"datname"},
				PromQL: `topk(12, sum by (datname) (pg_stat_activity_count{__SEL__,state="active"}))`},
			{Key: "idle_sessions", Title: "Idle sessions", Unit: "conns", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"datname"},
				PromQL: `topk(12, sum by (datname) (pg_stat_activity_count{__SEL__,state=~"idle|idle in transaction|idle in transaction \\(aborted\\)"}))`},
			// 对齐 Grafana：pg_locks_count{...} != 0，丢掉零锁序列，保留 datname/mode
			{Key: "lock_tables", Title: "Lock tables", Unit: "locks", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"datname", "mode"},
				PromQL: `topk(12, pg_locks_count{__SEL__} != 0)`},
			{Key: "temp_bytes", Title: "Temp File (Bytes)", Unit: "bytes", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"datname"},
				PromQL: `topk(12, sum by (datname) (rate(pg_stat_database_temp_bytes{__SEL__,datname!~"template.*|postgres"}[5m])))`},
			{Key: "deadlocks", Title: "Conflicts/Deadlocks", Unit: "/s", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"datname"},
				PromQL: `topk(12, sum by (datname) (rate(pg_stat_database_deadlocks{__SEL__,datname!~"template.*|postgres"}[5m]) + rate(pg_stat_database_conflicts{__SEL__,datname!~"template.*|postgres"}[5m])))`},
		},
	},
}
