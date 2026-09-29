package dbmonitor

// Cassandra 对齐社区 cassandra_exporter / JMX exporter 常见指标。
var cassandraSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "cass_up", Title: "Up", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(up{job=~".*cassandra.*",__SEL__} or cassandra_stats_total_hints{__SEL__} or org_apache_cassandra_metrics_client_connectedNativeClients{__SEL__})`},
			{Key: "cass_clients", Title: "Native Clients", Unit: "conns", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(org_apache_cassandra_metrics_client_connectedNativeClients{__SEL__} or cassandra_stats_connected_native_clients{__SEL__})`},
			{Key: "cass_load", Title: "Storage Load", Unit: "bytes", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(org_apache_cassandra_metrics_storage_load_count{__SEL__} or cassandra_storage_load{__SEL__} or cassandra_stats_load{__SEL__})`},
			{Key: "cass_pending", Title: "Compaction Pending", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(org_apache_cassandra_metrics_compaction_pendingtasks{__SEL__} or cassandra_compaction_pending_tasks{__SEL__})`},

			{Key: "cass_read_lat", Title: "Read Latency", Unit: "s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `org_apache_cassandra_metrics_clientrequest_latency{request_type="Read",quantile="0.99",__SEL__} or rate(org_apache_cassandra_metrics_clientrequest_totalLatency{request_type="Read",__SEL__}[5m]) / clamp_min(rate(org_apache_cassandra_metrics_clientrequest_latency_count{request_type="Read",__SEL__}[5m]), 1)`},
			{Key: "cass_write_lat", Title: "Write Latency", Unit: "s", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `org_apache_cassandra_metrics_clientrequest_latency{request_type="Write",quantile="0.99",__SEL__} or rate(org_apache_cassandra_metrics_clientrequest_totalLatency{request_type="Write",__SEL__}[5m]) / clamp_min(rate(org_apache_cassandra_metrics_clientrequest_latency_count{request_type="Write",__SEL__}[5m]), 1)`},
			{Key: "cass_throughput", Title: "Client Requests / sec", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"request_type"},
				PromQL: `sum by (request_type) (rate(org_apache_cassandra_metrics_clientrequest_latency_count{__SEL__}[5m]))`},
		},
	},
	{
		Name: "db_stats", Title: "Cache / Compaction",
		Panels: []PanelDef{
			{Key: "cass_cache", Title: "Key Cache Hit Rate", Unit: "ratio", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `org_apache_cassandra_metrics_cache_hitrate{cache="KeyCache",__SEL__} or cassandra_cache_hit_ratio{__SEL__}`},
			{Key: "cass_compaction_ts", Title: "Compaction Pending Tasks", Unit: "num", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `org_apache_cassandra_metrics_compaction_pendingtasks{__SEL__} or cassandra_compaction_pending_tasks{__SEL__}`},
		},
	},
	{
		Name: "sessions", Title: "JVM",
		Panels: []PanelDef{
			{Key: "cass_jvm_heap", Title: "JVM Heap Used", Unit: "bytes", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `jvm_memory_bytes_used{area="heap",__SEL__} or jvm_memory_used_bytes{area="heap",__SEL__}`},
			{Key: "cass_jvm_gc", Title: "GC Pause / sec", Unit: "s", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"gc"},
				PromQL: `sum by (gc) (rate(jvm_gc_collection_seconds_sum{__SEL__}[5m]))`},
		},
	},
}
