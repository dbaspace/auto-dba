package sqlinsight

import (
	"strings"

	"auto-dba/internal/store"
)

const (
	siSourceAWR        = "awr"
	siSourcePgActivity = "pg_activity"

	siExprBuiltin = "builtin_field"

	siRuleThreshold = "threshold"
	siRuleDoD       = "dod"
	siRuleBaseline  = "baseline"
	siRuleKindAWRImport = "awr_import"

	siDirAbove = "above"
	siDirBelow = "below"
	siDirSpike = "spike"
	siDirDrop  = "drop"

	siSevNone     = "none"
	siSevInfo     = "info"
	siSevWarning  = "warning"
	siSevCritical = "critical"

	siRunRunning   = "running"
	siRunSucceeded = "succeeded"
	siRunFailed    = "failed"
	siRunPartial   = "partial"

	siNarrativeSkipped   = "skipped"
	siNarrativeSucceeded = "succeeded"

	siBaselineMinSamples = 7
	siWindow7d           = "7d"
	siWindow30d          = "30d"

	siArgCronEnabled = "sql_insight_analysis_cron_enabled"
	siArgCronSpec    = "sql_insight_analysis_cron"
	siDefaultCronSpec = "0 0 3 * * *"
)

// Service SQL 洞察智能分析。
type Service struct {
	Store *store.DB
	AWR   AWRReportLoader
}

// New 创建服务。awr 可为 nil（使用空 stub）。
func New(db *store.DB, awr AWRReportLoader) *Service {
	if awr == nil {
		awr = StubAWRLoader{}
	}
	return &Service{Store: db, AWR: awr}
}

func builtinCatalog() []*BuiltinField {
	return []*BuiltinField{
		{Key: "cpu_usage", Name: "CPU 使用率", Source: siSourceAWR, Unit: "ratio", DefaultStat: "max", Description: "AWR CPU 使用率"},
		{Key: "mem_usage", Name: "内存使用率", Source: siSourceAWR, Unit: "ratio", DefaultStat: "max", Description: "AWR 内存使用率"},
		{Key: "disk_usage", Name: "磁盘使用率", Source: siSourceAWR, Unit: "ratio", DefaultStat: "max", Description: "AWR 磁盘使用率"},
		{Key: "conn_usage_ratio", Name: "连接使用率", Source: siSourceAWR, Unit: "ratio", DefaultStat: "max", Description: "连接数占 max_connections 比例"},
		{Key: "cache_hit_ratio", Name: "缓存命中率", Source: siSourceAWR, Unit: "ratio", DefaultStat: "min", Description: "shared_buffers 缓存命中率"},
		{Key: "replication_lag", Name: "复制延迟", Source: siSourceAWR, Unit: "s", DefaultStat: "max", Description: "主从复制延迟秒数"},
		{Key: "deadlocks_ps", Name: "死锁速率", Source: siSourceAWR, Unit: "/s", DefaultStat: "max", Description: "死锁每秒次数"},
		{Key: "slow_query_cnt", Name: "慢查询数", Source: siSourceAWR, Unit: "count", DefaultStat: "max", Description: "慢查询计数"},
		{Key: "tps", Name: "TPS", Source: siSourceAWR, Unit: "/s", DefaultStat: "avg", Description: "每秒事务数"},
		{Key: "backends", Name: "后端连接数", Source: siSourceAWR, Unit: "count", DefaultStat: "avg", Description: "活跃后端连接"},
		{Key: "max_xact_duration", Name: "最长事务时长", Source: siSourcePgActivity, Unit: "s", DefaultStat: "max", Description: "活动会话快照中最大事务时长（秒）"},
		{Key: "active_session_count", Name: "活跃会话数", Source: siSourcePgActivity, Unit: "count", DefaultStat: "max", Description: "非 idle 会话快照条数"},
	}
}

func builtinByKey(key string) *BuiltinField {
	k := strings.TrimSpace(key)
	for _, f := range builtinCatalog() {
		if f.Key == k {
			return f
		}
	}
	return nil
}

// Catalog 返回内置指标目录。
func (s *Service) Catalog() []*BuiltinField {
	return builtinCatalog()
}

// sqlInsightScenarioContext 用户场景（文案 + 标签）。
type sqlInsightScenarioContext struct {
	Text string
	Tags []string
}
