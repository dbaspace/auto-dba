package dbmonitor

// Oracle 对齐 iamseth/oracledb_exporter（oracledb_*）。
var oracleSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "oracle_up", Title: "Up", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(oracledb_up{__SEL__})`},
			{Key: "oracle_sessions", Title: "Active Sessions", Unit: "conns", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(oracledb_sessions_value{status="ACTIVE",__SEL__} or oracledb_activity_user_commits{__SEL__})`},
			{Key: "oracle_process", Title: "Processes", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(oracledb_process_count{__SEL__})`},
			{Key: "oracle_exec", Title: "Execute Count/s", Unit: "ops", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(oracledb_activity_execute_count{__SEL__}[5m]))`},

			{Key: "oracle_activity_ts", Title: "Activity Rates", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(rate(oracledb_activity_execute_count{__SEL__}[5m]), "kind", "execute", "", "") or label_replace(rate(oracledb_activity_parse_count_total{__SEL__}[5m]), "kind", "parse", "", "") or label_replace(rate(oracledb_activity_user_commits{__SEL__}[5m]), "kind", "commit", "", "") or label_replace(rate(oracledb_activity_user_rollbacks{__SEL__}[5m]), "kind", "rollback", "", "")`},
			{Key: "oracle_sessions_ts", Title: "Sessions by Status", Unit: "conns", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"status"},
				PromQL: `oracledb_sessions_value{__SEL__}`},
			{Key: "oracle_wait_ts", Title: "Wait Time", Unit: "s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"wait_class"},
				PromQL: `sum by (wait_class) (rate(oracledb_wait_time_value{__SEL__}[5m]))`},
		},
	},
	{
		Name: "db_stats", Title: "Tablespace",
		Panels: []PanelDef{
			{Key: "oracle_tbs_used", Title: "Tablespace Used Bytes", Unit: "bytes", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"tablespace"},
				PromQL: `oracledb_tablespace_bytes{type="used",__SEL__} or oracledb_tablespace_used_bytes{__SEL__}`},
			{Key: "oracle_tbs_free", Title: "Tablespace Free Bytes", Unit: "bytes", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"tablespace"},
				PromQL: `oracledb_tablespace_bytes{type="free",__SEL__} or oracledb_tablespace_free_bytes{__SEL__}`},
		},
	},
	{
		Name: "sessions", Title: "Resource",
		Panels: []PanelDef{
			{Key: "oracle_resource", Title: "Resource Limit Current", Unit: "num", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"resource_name"},
				PromQL: `oracledb_resource_current_utilization{__SEL__}`},
		},
	},
}
