package dbmonitor

// SQL Server 对齐常见 mssql_exporter / sqlserver_exporter（mssql_* / sql_*）。
var sqlserverSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "mssql_up", Title: "Up", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(mssql_up{__SEL__} or sql_up{__SEL__})`},
			{Key: "mssql_conns", Title: "Connections", Unit: "conns", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(mssql_connections{__SEL__} or mssql_connections_total{__SEL__})`},
			{Key: "mssql_batch", Title: "Batch Requests/s", Unit: "ops", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(mssql_batch_requests{__SEL__}[5m]) or rate(mssql_batch_requests_total{__SEL__}[5m]))`},
			{Key: "mssql_ple", Title: "Page Life Expectancy", Unit: "s", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(mssql_page_life_expectancy{__SEL__} or mssql_performance_counters{object="SQLServer:Buffer Manager",counter="Page life expectancy",__SEL__})`},

			{Key: "mssql_batch_ts", Title: "Batch Requests / sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(mssql_batch_requests{__SEL__}[5m]) or rate(mssql_batch_requests_total{__SEL__}[5m]))`},
			{Key: "mssql_conn_ts", Title: "Connections", Unit: "conns", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `mssql_connections{__SEL__} or mssql_connections_total{__SEL__}`},
			{Key: "mssql_deadlock_ts", Title: "Deadlocks / sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(mssql_deadlocks{__SEL__}[5m]) or rate(mssql_deadlocks_total{__SEL__}[5m]))`},
		},
	},
	{
		Name: "db_stats", Title: "Buffer / IO",
		Panels: []PanelDef{
			{Key: "mssql_buffer_hit", Title: "Buffer Cache Hit Ratio", Unit: "percent", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `mssql_buffer_cache_hit_ratio{__SEL__} or mssql_performance_counters{object="SQLServer:Buffer Manager",counter="Buffer cache hit ratio",__SEL__}`},
			{Key: "mssql_io", Title: "Page Reads/Writes / sec", Unit: "/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(rate(mssql_page_reads_total{__SEL__}[5m]), "kind", "reads", "", "") or label_replace(rate(mssql_page_writes_total{__SEL__}[5m]), "kind", "writes", "", "")`},
		},
	},
	{
		Name: "sessions", Title: "Waits",
		Panels: []PanelDef{
			{Key: "mssql_waits", Title: "Wait Stats / sec", Unit: "/s", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"wait_type"},
				PromQL: `topk(15, sum by (wait_type) (rate(mssql_os_wait_stats_seconds_total{__SEL__}[5m]) or rate(mssql_wait_time_ms{__SEL__}[5m])))`},
		},
	},
}
