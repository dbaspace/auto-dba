package dbmonitor

// OceanBase 对齐 Grafana 15215 / obagent（ob_sysstat / ob_*）。
var oceanbaseSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "ob_sessions", Title: "Active Sessions", Unit: "conns", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(ob_active_session_num{__SEL__})`},
			{Key: "ob_sql_qps", Title: "SQL QPS", Unit: "qps", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(ob_sysstat{stat_id="40002",__SEL__}[5m])) + sum(rate(ob_sysstat{stat_id="40004",__SEL__}[5m])) + sum(rate(ob_sysstat{stat_id="40006",__SEL__}[5m])) + sum(rate(ob_sysstat{stat_id="40008",__SEL__}[5m])) + sum(rate(ob_sysstat{stat_id="40000",__SEL__}[5m]))`},
			{Key: "ob_tps", Title: "TPS", Unit: "ops", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(ob_sysstat{stat_id="40012",__SEL__}[5m]))`},
			{Key: "ob_trans_rt", Title: "Transaction RT", Unit: "us", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(ob_sysstat{stat_id="40010",__SEL__}[5m])) / clamp_min(sum(rate(ob_sysstat{stat_id="40012",__SEL__}[5m])), 1)`},

			{Key: "ob_sql_ts", Title: "SQL QPS by Type", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(sum(rate(ob_sysstat{stat_id="40002",__SEL__}[5m])), "kind", "select", "", "") or label_replace(sum(rate(ob_sysstat{stat_id="40004",__SEL__}[5m])), "kind", "insert", "", "") or label_replace(sum(rate(ob_sysstat{stat_id="40006",__SEL__}[5m])), "kind", "replace", "", "") or label_replace(sum(rate(ob_sysstat{stat_id="40008",__SEL__}[5m])), "kind", "update", "", "") or label_replace(sum(rate(ob_sysstat{stat_id="40000",__SEL__}[5m])), "kind", "delete", "", "")`},
			{Key: "ob_session_ts", Title: "Active Sessions", Unit: "conns", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"tenant_name", "svr_ip"},
				PromQL: `sum by (tenant_name) (ob_active_session_num{__SEL__})`},
			{Key: "ob_wait_ts", Title: "Wait Events / sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"event"},
				PromQL: `topk(10, sum by (event) (rate(ob_waitevent_wait_total{__SEL__}[5m])))`},
		},
	},
	{
		Name: "db_stats", Title: "Cache / IO",
		Panels: []PanelDef{
			{Key: "ob_cache", Title: "Cache Size", Unit: "bytes", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"cache_name"},
				PromQL: `sum by (cache_name) (ob_cache_size_bytes{__SEL__})`},
			{Key: "ob_plan_cache", Title: "Plan Cache Hit Ratio", Unit: "ratio", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"tenant_name"},
				PromQL: `sum(rate(ob_plan_cache_hit_total{__SEL__}[5m])) / clamp_min(sum(rate(ob_plan_cache_access_total{__SEL__}[5m])), 1)`},
			{Key: "ob_io", Title: "IO Bytes / sec", Unit: "bytes/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(sum(rate(ob_sysstat{stat_id="60000",__SEL__}[5m])), "kind", "read", "", "") or label_replace(sum(rate(ob_sysstat{stat_id="60003",__SEL__}[5m])), "kind", "write", "", "")`},
		},
	},
	{
		Name: "sessions", Title: "Memory",
		Panels: []PanelDef{
			{Key: "ob_mem", Title: "Memory Used / Limit", Unit: "bytes", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(sum(ob_sysstat{stat_id="130001",__SEL__}), "kind", "used", "", "") or label_replace(sum(ob_sysstat{stat_id="130004",__SEL__}), "kind", "limit", "", "") or label_replace(sum(ob_sysstat{stat_id="130000",__SEL__}), "kind", "hold", "", "")`},
		},
	},
}
