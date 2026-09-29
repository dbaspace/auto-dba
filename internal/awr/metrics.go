package awr

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	awrPromSelToken = "__SEL__"

	awrScopeDB   = "db"
	awrScopeHost = "host"

	awrSourceSelfPG     = "self_pg"
	awrSourceTencentQCE = "tencent_qce"
	awrSourceUnknown    = "unknown"
)

type awrMetricDef struct {
	Key     string
	Title   string
	Unit    string
	Scope   string
	PromQL  string
	Instant bool
}

type awrMetricSection struct {
	Name    string
	Title   string
	Metrics []awrMetricDef
}

var awrSelfPGSections = []awrMetricSection{
	{
		Name: "load_profile", Title: "Load Profile",
		Metrics: []awrMetricDef{
			{Key: "tps", Title: "TPS（提交+回滚速率）", Unit: "tx/s",
				PromQL: `sum(rate(pg_stat_database_xact_commit{__SEL__}[5m])) + sum(rate(pg_stat_database_xact_rollback{__SEL__}[5m]))`},
			{Key: "commit_ps", Title: "提交速率", Unit: "tx/s",
				PromQL: `sum(rate(pg_stat_database_xact_commit{__SEL__}[5m]))`},
			{Key: "rollback_ps", Title: "回滚速率", Unit: "tx/s",
				PromQL: `sum(rate(pg_stat_database_xact_rollback{__SEL__}[5m]))`},
			{Key: "tup_fetched_ps", Title: "行读取速率", Unit: "rows/s",
				PromQL: `sum(rate(pg_stat_database_tup_fetched{__SEL__}[5m]))`},
			{Key: "tup_returned_ps", Title: "行返回速率", Unit: "rows/s",
				PromQL: `sum(rate(pg_stat_database_tup_returned{__SEL__}[5m]))`},
			{Key: "tup_inserted_ps", Title: "行插入速率", Unit: "rows/s",
				PromQL: `sum(rate(pg_stat_database_tup_inserted{__SEL__}[5m]))`},
			{Key: "tup_updated_ps", Title: "行更新速率", Unit: "rows/s",
				PromQL: `sum(rate(pg_stat_database_tup_updated{__SEL__}[5m]))`},
			{Key: "tup_deleted_ps", Title: "行删除速率", Unit: "rows/s",
				PromQL: `sum(rate(pg_stat_database_tup_deleted{__SEL__}[5m]))`},
		},
	},
	{
		Name: "cache_hit", Title: "缓存命中率",
		Metrics: []awrMetricDef{
			{Key: "cache_hit_ratio", Title: "缓存命中率", Unit: "ratio",
				PromQL: `sum(rate(pg_stat_database_blks_hit{__SEL__}[5m])) / (sum(rate(pg_stat_database_blks_hit{__SEL__}[5m])) + sum(rate(pg_stat_database_blks_read{__SEL__}[5m])))`},
		},
	},
	{
		Name: "connections", Title: "连接",
		Metrics: []awrMetricDef{
			{Key: "backends", Title: "后端连接数", Unit: "conns",
				PromQL: `sum(pg_stat_database_numbackends{__SEL__})`},
			{Key: "max_connections", Title: "最大连接数", Unit: "conns",
				PromQL: `max(pg_settings_max_connections{__SEL__})`},
			{Key: "conn_usage_ratio", Title: "连接使用率", Unit: "ratio",
				PromQL: `sum(pg_stat_database_numbackends{__SEL__}) / max(pg_settings_max_connections{__SEL__})`},
		},
	},
	{
		Name: "locks", Title: "锁 / 死锁",
		Metrics: []awrMetricDef{
			{Key: "locks", Title: "锁数量", Unit: "locks",
				PromQL: `sum(pg_locks_count{__SEL__})`},
			{Key: "deadlocks_ps", Title: "死锁速率", Unit: "/s",
				PromQL: `sum(rate(pg_stat_database_deadlocks{__SEL__}[5m]))`},
		},
	},
	{
		Name: "replication", Title: "复制延迟 / 槽",
		Metrics: []awrMetricDef{
			{Key: "replication_lag", Title: "复制延迟", Unit: "s",
				PromQL: `max(pg_replication_lag_seconds{__SEL__})`},
			{Key: "replication_slots", Title: "活跃复制槽数量", Unit: "slots",
				PromQL: `sum(pg_replication_slots_active{__SEL__})`},
		},
	},
	{
		Name: "checkpoint", Title: "Checkpoint / BgWriter",
		Metrics: []awrMetricDef{
			{Key: "checkpoints_timed_ps", Title: "定时 checkpoint 速率", Unit: "/s",
				PromQL: `sum(rate(pg_stat_bgwriter_checkpoints_timed_total{__SEL__}[5m]))`},
			{Key: "checkpoints_req_ps", Title: "请求 checkpoint 速率", Unit: "/s",
				PromQL: `sum(rate(pg_stat_bgwriter_checkpoints_req_total{__SEL__}[5m]))`},
			{Key: "buffers_checkpoint_ps", Title: "checkpoint 写缓冲速率", Unit: "buffers/s",
				PromQL: `sum(rate(pg_stat_bgwriter_buffers_checkpoint_total{__SEL__}[5m]))`},
		},
	},
	{
		Name: "db_size", Title: "库大小",
		Metrics: []awrMetricDef{
			{Key: "db_size_total", Title: "实例库总大小", Unit: "bytes", Instant: true,
				PromQL: `sum(pg_database_size_bytes{__SEL__})`},
		},
	},
	{
		Name: "host", Title: "主机资源",
		Metrics: []awrMetricDef{
			{Key: "cpu_usage", Title: "CPU 使用率", Unit: "ratio", Scope: awrScopeHost,
				PromQL: `1 - avg(rate(node_cpu_seconds_total{mode="idle",__SEL__}[5m]))`},
			{Key: "mem_usage", Title: "内存使用率", Unit: "ratio", Scope: awrScopeHost,
				PromQL: `1 - (avg(node_memory_MemAvailable_bytes{__SEL__}) / avg(node_memory_MemTotal_bytes{__SEL__}))`},
			{Key: "disk_usage", Title: "根分区磁盘使用率", Unit: "ratio", Scope: awrScopeHost,
				PromQL: `1 - (avg(node_filesystem_avail_bytes{mountpoint="/",__SEL__}) / avg(node_filesystem_size_bytes{mountpoint="/",__SEL__}))`},
			{Key: "disk_read_ps", Title: "磁盘读速率", Unit: "bytes/s", Scope: awrScopeHost,
				PromQL: `sum(rate(node_disk_read_bytes_total{__SEL__}[5m]))`},
			{Key: "disk_write_ps", Title: "磁盘写速率", Unit: "bytes/s", Scope: awrScopeHost,
				PromQL: `sum(rate(node_disk_written_bytes_total{__SEL__}[5m]))`},
		},
	},
}

// awrTencentSections 腾讯云监控指标（默认探测关闭，仅当检测到 qce 且未优先 self_pg 时使用）。
var awrTencentSections = []awrMetricSection{
	{
		Name: "host", Title: "资源使用（云监控）",
		Metrics: []awrMetricDef{
			{Key: "cpu_usage", Title: "CPU 使用率", Unit: "ratio",
				PromQL: `max(qce_postgres_cpu_max{__SEL__}) / 100`},
			{Key: "mem_usage", Title: "内存使用率", Unit: "ratio",
				PromQL: `max(qce_postgres_memoryrate_max{__SEL__}) / 100`},
			{Key: "disk_usage", Title: "磁盘（存储）使用率", Unit: "ratio",
				PromQL: `max(qce_postgres_storagerate_max{__SEL__}) / 100`},
		},
	},
	{
		Name: "connections", Title: "连接",
		Metrics: []awrMetricDef{
			{Key: "backends", Title: "连接数", Unit: "conns",
				PromQL: `max(qce_postgres_connections_avg{__SEL__})`},
			{Key: "active_conns", Title: "活跃连接数", Unit: "conns",
				PromQL: `max(qce_postgres_activeconns_max{__SEL__})`},
			{Key: "idle_conns", Title: "空闲连接数", Unit: "conns",
				PromQL: `max(qce_postgres_idleconns_avg{__SEL__})`},
			{Key: "conn_usage_ratio", Title: "连接使用率", Unit: "ratio",
				PromQL: `max(qce_postgres_connutilization_max{__SEL__}) / 100`},
		},
	},
	{
		Name: "load_profile", Title: "负载 / 慢查询（云监控）",
		Metrics: []awrMetricDef{
			{Key: "slow_query_cnt", Title: "慢查询数", Unit: "count",
				PromQL: `max(qce_postgres_slowquerycnt_max{__SEL__})`},
			{Key: "sql_runtime_max", Title: "SQL 最长执行时长", Unit: "s",
				PromQL: `max(qce_postgres_sqlruntimemax_avg{__SEL__})`},
			{Key: "sql_runtime_min", Title: "SQL 最短执行时长", Unit: "s",
				PromQL: `max(qce_postgres_sqlruntimemin_avg{__SEL__})`},
		},
	},
}

var awrCloudResourceMetrics = []awrMetricDef{
	{Key: "cpu_usage", Title: "CPU 使用率", Unit: "ratio",
		PromQL: `max(qce_postgres_cpu_max{__SEL__}) / 100`},
	{Key: "mem_usage", Title: "内存使用率", Unit: "ratio",
		PromQL: `max(qce_postgres_memoryrate_max{__SEL__}) / 100`},
	{Key: "disk_usage", Title: "存储使用率", Unit: "ratio",
		PromQL: `max(qce_postgres_storagerate_max{__SEL__}) / 100`},
	{Key: "mem_used", Title: "内存使用量", Unit: "MB",
		PromQL: `max(qce_postgres_memory_max{__SEL__})`},
	{Key: "storage_used", Title: "存储使用量", Unit: "GB",
		PromQL: `max(qce_postgres_storage_max{__SEL__})`},
	{Key: "data_disk", Title: "数据盘用量", Unit: "GB",
		PromQL: `max(qce_postgres_datafilesize_max{__SEL__})`},
	{Key: "log_disk", Title: "日志盘用量", Unit: "GB",
		PromQL: `max(qce_postgres_logfilesize_max{__SEL__})`},
}

func awrSectionsForSource(source string) []awrMetricSection {
	switch source {
	case awrSourceTencentQCE:
		return awrTencentSections
	case awrSourceSelfPG:
		return awrSelfPGSections
	default:
		return nil
	}
}

func (s *Service) awrCloudSelector(instanceName string) string {
	label := s.stringArg(awrPromCloudLabelKey, "d_b_instance_name")
	return fmt.Sprintf(`%s=%q`, label, instanceName)
}

func (s *Service) awrDBSelector(domainHost string) string {
	// 与 seed 默认 awr_prom_instance_label=id 对齐。
	label := s.stringArg(awrPromDBLabelKey, "id")
	return fmt.Sprintf(`%s=%q`, label, domainHost)
}

func (s *Service) awrHostSelector(hostIP string) string {
	label := s.stringArg(awrPromHostLabelKey, "instance")
	// PromQL "..." 中 \. 非法，QuoteMeta 后需把 \ 加倍
	re := strings.ReplaceAll(regexp.QuoteMeta(hostIP), `\`, `\\`)
	return fmt.Sprintf(`%s=~"%s:.*"`, label, re)
}

func awrRenderPromQL(tmpl, selector string) string {
	return strings.ReplaceAll(tmpl, awrPromSelToken, selector)
}
