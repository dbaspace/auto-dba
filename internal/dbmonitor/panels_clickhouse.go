package dbmonitor

// ClickHouse（Grafana uid b8N6Wsy70）。
var clickhouseSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "ch_uptime", Title: "Uptime", Unit: "seconds", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(ClickHouseAsyncMetrics_Uptime{__SEL__})`},
			{Key: "ch_bytes", Title: "TotalBytesOfMergeTreeTables", Unit: "bytes", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(ClickHouseAsyncMetrics_TotalBytesOfMergeTreeTables{__SEL__})`},
			{Key: "ch_rows", Title: "TotalRowsOfMergeTreeTables", Unit: "rows", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(ClickHouseAsyncMetrics_TotalRowsOfMergeTreeTables{__SEL__})`},
			{Key: "ch_cpu", Title: "IdleTimeCPU (busy)", Unit: "ratio", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `1 - avg by (instance) ({__name__=~"ClickHouseAsyncMetrics_OSIdleTimeCPU.*",__SEL__})`},
			{Key: "ch_space", Title: "空间占用情况", Unit: "bytes", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `max by (instance) (ClickHouseAsyncMetrics_TotalBytesOfMergeTreeTables{__SEL__})`},
			{Key: "ch_rows_ts", Title: "数据行趋势", Unit: "rows", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `max by (instance) (ClickHouseAsyncMetrics_TotalRowsOfMergeTreeTables{__SEL__})`},
			{Key: "ch_fail", Title: "读取写入失败", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"domain"},
				PromQL: `sum by (domain) (rate(ClickHouseProfileEvents_FailedSelectQuery{__SEL__}[5m])) + sum by (domain) (rate(ClickHouseProfileEvents_FailedInsertQuery{__SEL__}[5m]))`},
		},
	},
	{
		Name: "settings", Title: "Disk",
		Panels: []PanelDef{
			{Key: "ch_disk_hot", Title: "热盘使用情况", Unit: "bytes", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(ClickHouseAsyncMetrics_DiskUsed_disk01{__SEL__})`},
			{Key: "ch_disk_root", Title: "根盘使用情况", Unit: "bytes", Section: "settings", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(ClickHouseAsyncMetrics_DiskUsed_default{__SEL__})`},
		},
	},
	{
		Name: "db_stats", Title: "Query / Insert",
		Panels: []PanelDef{
			{Key: "ch_query", Title: "TotalQuery/M", Unit: "/m", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(ClickHouseProfileEvents_Query{__SEL__}[2m]) * 60)`},
			{Key: "ch_select", Title: "SelectQuery/M", Unit: "/m", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(ClickHouseProfileEvents_SelectQuery{__SEL__}[2m]) * 60)`},
			{Key: "ch_insert", Title: "InsertNumbers/M", Unit: "/m", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(ClickHouseProfileEvents_InsertQuery{__SEL__}[2m]) * 60)`},
			{Key: "ch_inserted_rows", Title: "InsertedRows/M", Unit: "rows/m", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(ClickHouseProfileEvents_InsertedRows{__SEL__}[2m]) * 60)`},
		},
	},
	{
		Name: "sessions", Title: "Merge / Cache",
		Panels: []PanelDef{
			{Key: "ch_merge", Title: "Merge numbers", Unit: "/m", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(ClickHouseProfileEvents_Merge{__SEL__}[2m]) * 60)`},
			{Key: "ch_merge_rows", Title: "Merge rows", Unit: "rows/m", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(ClickHouseProfileEvents_MergedRows{__SEL__}[2m]) * 60)`},
			{Key: "ch_mark_cache", Title: "MarkCache Misses/Hits", Unit: "/m", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(ClickHouseProfileEvents_MarkCacheMisses{__SEL__}[2m]) * 60)`},
		},
	},
}
