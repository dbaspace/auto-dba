package dbmonitor

// CockroachDB 对齐官方 by-cluster/sql.json + runtime.json（/_status/vars）。
var cockroachSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "crdb_uptime", Title: "Uptime", Unit: "s", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(sys_uptime{__SEL__})`},
			{Key: "crdb_conns", Title: "SQL Connections", Unit: "conns", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(sql_conns{__SEL__})`},
			{Key: "crdb_qps", Title: "SQL Statements/s", Unit: "qps", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(sql_select_count{__SEL__}[5m]) + rate(sql_insert_count{__SEL__}[5m]) + rate(sql_update_count{__SEL__}[5m]) + rate(sql_delete_count{__SEL__}[5m]))`},
			{Key: "crdb_cpu", Title: "CPU Combined %", Unit: "percent", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `100 * avg(sys_cpu_combined_percent_normalized{__SEL__})`},

			{Key: "crdb_sql_ts", Title: "SQL Statements / sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(sum(rate(sql_select_count{__SEL__}[5m])), "kind", "select", "", "") or label_replace(sum(rate(sql_insert_count{__SEL__}[5m])), "kind", "insert", "", "") or label_replace(sum(rate(sql_update_count{__SEL__}[5m])), "kind", "update", "", "") or label_replace(sum(rate(sql_delete_count{__SEL__}[5m])), "kind", "delete", "", "")`},
			{Key: "crdb_conn_ts", Title: "SQL Connections", Unit: "conns", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sql_conns{__SEL__}`},
			{Key: "crdb_latency_ts", Title: "SQL Service Latency p99", Unit: "s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `histogram_quantile(0.99, sum by (le, instance) (rate(sql_service_latency_bucket{__SEL__}[5m])))`},
		},
	},
	{
		Name: "db_stats", Title: "Storage / Capacity",
		Panels: []PanelDef{
			{Key: "crdb_capacity", Title: "Capacity Used / Available", Unit: "bytes", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(sum(capacity_used{__SEL__}), "kind", "used", "", "") or label_replace(sum(capacity_available{__SEL__}), "kind", "available", "", "") or label_replace(sum(capacity{__SEL__}), "kind", "total", "", "")`},
			{Key: "crdb_rss", Title: "RSS Memory", Unit: "bytes", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sys_rss{__SEL__}`},
		},
	},
	{
		Name: "sessions", Title: "Replication",
		Panels: []PanelDef{
			{Key: "crdb_replicas", Title: "Replicas / Ranges", Unit: "num", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(sum(replicas{__SEL__}), "kind", "replicas", "", "") or label_replace(sum(ranges{__SEL__}), "kind", "ranges", "", "") or label_replace(sum(ranges_underreplicated{__SEL__}), "kind", "underreplicated", "", "")`},
		},
	},
}
