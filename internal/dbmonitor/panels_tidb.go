package dbmonitor

// TiDB 对齐 PingCAP overview / tidb.json（tidb_*）。
var tidbSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "tidb_conns", Title: "Connections", Unit: "conns", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(tidb_server_connections{__SEL__})`},
			{Key: "tidb_qps", Title: "QPS", Unit: "qps", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(tidb_executor_statement_total{__SEL__}[5m]))`},
			{Key: "tidb_latency_p99", Title: "Query Duration p99", Unit: "s", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `histogram_quantile(0.99, sum(rate(tidb_server_handle_query_duration_seconds_bucket{__SEL__}[5m])) by (le))`},
			{Key: "tidb_err", Title: "Execute Errors/s", Unit: "/s", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(rate(tidb_server_execute_error_total{__SEL__}[5m]))`},

			{Key: "tidb_stmt_ts", Title: "Statements / sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"type"},
				PromQL: `sum by (type) (rate(tidb_executor_statement_total{__SEL__}[5m]))`},
			{Key: "tidb_conn_ts", Title: "Connections", Unit: "conns", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `tidb_server_connections{__SEL__}`},
			{Key: "tidb_latency_ts", Title: "Query Duration p95/p99", Unit: "s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(histogram_quantile(0.95, sum(rate(tidb_server_handle_query_duration_seconds_bucket{__SEL__}[5m])) by (le)), "kind", "p95", "", "") or label_replace(histogram_quantile(0.99, sum(rate(tidb_server_handle_query_duration_seconds_bucket{__SEL__}[5m])) by (le)), "kind", "p99", "", "")`},
		},
	},
	{
		Name: "db_stats", Title: "Transaction / TiKV Client",
		Panels: []PanelDef{
			{Key: "tidb_txn_ts", Title: "Transaction Duration", Unit: "s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"type"},
				PromQL: `histogram_quantile(0.99, sum by (le, type) (rate(tidb_session_transaction_duration_seconds_bucket{__SEL__}[5m])))`},
			{Key: "tidb_tikv_backoff", Title: "TiKV Client Backoff", Unit: "/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"type"},
				PromQL: `sum by (type) (rate(tidb_tikvclient_backoff_seconds_count{__SEL__}[5m]) or rate(tidb_tikvclient_backoff_total{__SEL__}[5m]))`},
		},
	},
	{
		Name: "sessions", Title: "Runtime",
		Panels: []PanelDef{
			{Key: "tidb_mem_ts", Title: "Process Memory", Unit: "bytes", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `process_resident_memory_bytes{__SEL__}`},
			{Key: "tidb_goroutines", Title: "Goroutines", Unit: "num", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `go_goroutines{__SEL__}`},
		},
	},
}
