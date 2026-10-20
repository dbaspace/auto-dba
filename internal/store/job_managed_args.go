package store

import "strings"

// JobManagedGlobalArgKeys 由定时任务中心独占写入的全局参数；禁止经 /global-args 与洞察设置页改调度。
var JobManagedGlobalArgKeys = map[string]string{
	"awr_enabled":                       "awr.daily",
	"awr_daily_cron":                    "awr.daily",
	"pg_space_enabled":                  "pgspace.daily",
	"pg_space_daily_cron":               "pgspace.daily",
	"sql_insight_analysis_cron_enabled": "sqlinsight.daily",
	"sql_insight_analysis_cron":         "sqlinsight.daily",
	ArgSlotMonitorCollectIntervalSec:    "sync.slots.collect",
}

func IsJobManagedGlobalArg(key string) bool {
	_, ok := JobManagedGlobalArgKeys[strings.TrimSpace(key)]
	return ok
}

func JobKeyForGlobalArg(key string) string {
	return JobManagedGlobalArgKeys[strings.TrimSpace(key)]
}
