package dbmonitor

// MongoDB 对齐 Percona mongodb_exporter（mongodb_* / mongodb_ss_*）。
var mongodbSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "mongo_up", Title: "Up", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(mongodb_up{__SEL__})`},
			{Key: "mongo_conns", Title: "Connections", Unit: "conns", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(mongodb_ss_connections{conn_type="current",__SEL__} or mongodb_connections{state="current",__SEL__} or mongodb_ss_connections{__SEL__})`},
			{Key: "mongo_mem", Title: "Resident Memory", Unit: "bytes", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(mongodb_ss_mem_resident{__SEL__} or mongodb_memory{type="resident",__SEL__}) * 1024 * 1024`},
			{Key: "mongo_ops", Title: "Opcounters/s", Unit: "ops", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(mongodb_ss_opcounters{__SEL__}[5m]) or rate(mongodb_opcounters_total{__SEL__}[5m]) or rate(mongodb_mongod_metrics_operation_total{__SEL__}[5m]))`},

			{Key: "mongo_ops_ts", Title: "Operations / sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"type", "legacy_op_type", "state"},
				PromQL: `sum by (type) (rate(mongodb_ss_opcounters{__SEL__}[5m])) or sum by (legacy_op_type) (rate(mongodb_opcounters_total{__SEL__}[5m])) or sum by (state) (rate(mongodb_mongod_op_counters_total{__SEL__}[5m]))`},
			{Key: "mongo_net_ts", Title: "Network I/O", Unit: "bytes/s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(rate(mongodb_ss_network_bytesIn{__SEL__}[5m]) or rate(mongodb_network_bytes_total{state="in_bytes",__SEL__}[5m]), "kind", "in", "", "") or label_replace(rate(mongodb_ss_network_bytesOut{__SEL__}[5m]) or rate(mongodb_network_bytes_total{state="out_bytes",__SEL__}[5m]), "kind", "out", "", "")`},
			{Key: "mongo_conn_ts", Title: "Connections", Unit: "conns", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"conn_type", "state"},
				PromQL: `mongodb_ss_connections{__SEL__} or mongodb_connections{__SEL__}`},
		},
	},
	{
		Name: "db_stats", Title: "Replication / WiredTiger",
		Panels: []PanelDef{
			{Key: "mongo_repl_lag", Title: "Replication Lag", Unit: "s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"member_idx", "name"},
				PromQL: `mongodb_mongod_replset_member_replication_lag{__SEL__} or mongodb_rs_members_lag{__SEL__}`},
			{Key: "mongo_wt_cache", Title: "WiredTiger Cache Used", Unit: "bytes", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `mongodb_ss_wt_cache_bytes_currently_in_cache{__SEL__} or mongodb_mongod_wiredtiger_cache_bytes{type="total",__SEL__}`},
		},
	},
	{
		Name: "sessions", Title: "Asserts / Queues",
		Panels: []PanelDef{
			{Key: "mongo_asserts", Title: "Asserts / sec", Unit: "/s", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"type"},
				PromQL: `sum by (type) (rate(mongodb_ss_asserts{__SEL__}[5m]) or rate(mongodb_asserts_total{__SEL__}[5m]))`},
			{Key: "mongo_queued", Title: "Queued Readers/Writers", Unit: "num", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"type"},
				PromQL: `mongodb_ss_globalLock_currentQueue{__SEL__} or mongodb_mongod_global_lock_current_queue{__SEL__}`},
		},
	},
}
