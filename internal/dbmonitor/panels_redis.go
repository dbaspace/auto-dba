package dbmonitor

// Redis 看板对齐 Grafana uid 59j7eSRWk2（gnetId 763 / redis_exporter）：
// 变量链 idc → domain → instance；面板过滤 __SEL__ = instance="ip:port"。
var redisSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "redis_uptime", Title: "Uptime", Unit: "seconds", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(redis_uptime_in_seconds{__SEL__})`},
			{Key: "redis_qps", Title: "QPS", Unit: "ops", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(irate(redis_commands_processed_total{__SEL__}[2m]))`},
			{Key: "redis_mem_max", Title: "最大内存", Unit: "bytes", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(redis_memory_max_bytes{__SEL__} or redis_config_maxmemory{__SEL__})`},
			{Key: "redis_mem_util", Title: "Memory Usage", Unit: "percent", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `100 * (max(redis_memory_used_bytes{__SEL__}) / clamp_min(max(redis_memory_max_bytes{__SEL__}), 1))`},

			{Key: "redis_hits", Title: "Hits / Misses per Sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(irate(redis_keyspace_hits_total{__SEL__}[5m]), "kind", "hits", "", "") or label_replace(irate(redis_keyspace_misses_total{__SEL__}[5m]), "kind", "misses", "", "")`},
			{Key: "redis_commands", Title: "Commands Executed / sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `rate(redis_commands_processed_total{__SEL__}[5m])`},
			{Key: "redis_net", Title: "Network I/O", Unit: "bytes/s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(rate(redis_net_input_bytes_total{__SEL__}[5m]), "kind", "input", "", "") or label_replace(rate(redis_net_output_bytes_total{__SEL__}[5m]), "kind", "output", "", "")`},
			{Key: "redis_mem_ts", Title: "Total Memory Usage", Unit: "bytes", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(redis_memory_used_bytes{__SEL__}, "kind", "used", "", "") or label_replace(redis_memory_max_bytes{__SEL__}, "kind", "max", "", "")`},
			{Key: "redis_clients", Title: "Clients", Unit: "conns", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(redis_connected_clients{__SEL__}, "kind", "current connect", "", "") or label_replace(redis_config_maxclients{__SEL__}, "kind", "maxclient", "", "")`},
		},
	},
	{
		Name: "db_stats", Title: "Keys & Expiry",
		Panels: []PanelDef{
			{Key: "redis_db_keys", Title: "Total Items per DB", Unit: "short", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"db"},
				// topk 限制 DB 数；query_range 前会 pin db 标签避免并集膨胀
				PromQL: `topk(12, sum by (db) (redis_db_keys{__SEL__}))`},
			{Key: "redis_expiry", Title: "Expiring vs Not-Expiring Keys", Unit: "short", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				// 聚合到实例级两条序列，避免 label_replace+or 双路 range
				PromQL: `label_replace(sum(redis_db_keys_expiring{__SEL__}), "kind", "expiring", "", "") or label_replace(sum(redis_db_keys{__SEL__}) - sum(redis_db_keys_expiring{__SEL__}), "kind", "not expiring", "", "")`},
			{Key: "redis_expired", Title: "Expired / Evicted", Unit: "/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(sum(rate(redis_expired_keys_total{__SEL__}[5m])), "kind", "expired", "", "") or label_replace(sum(rate(redis_evicted_keys_total{__SEL__}[5m])), "kind", "evicted", "", "")`},
		},
	},
}
