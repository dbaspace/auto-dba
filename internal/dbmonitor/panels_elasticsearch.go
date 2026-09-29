package dbmonitor

// Elasticsearch 对齐 prometheus-community/elasticsearch_exporter。
var elasticsearchSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "es_nodes", Title: "Nodes", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(elasticsearch_cluster_health_number_of_nodes{__SEL__})`},
			{Key: "es_data_nodes", Title: "Data Nodes", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(elasticsearch_cluster_health_number_of_data_nodes{__SEL__})`},
			{Key: "es_shards", Title: "Active Shards", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(elasticsearch_cluster_health_active_shards{__SEL__})`},
			{Key: "es_docs", Title: "Docs", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(elasticsearch_indices_docs_primary{__SEL__} or elasticsearch_indices_docs{__SEL__})`},

			{Key: "es_health_ts", Title: "Cluster Health Status", Unit: "num", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"color"},
				PromQL: `elasticsearch_cluster_health_status{__SEL__}`},
			{Key: "es_jvm_ts", Title: "JVM Heap Used", Unit: "bytes", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"name", "instance"},
				PromQL: `sum by (name) (elasticsearch_jvm_memory_used_bytes{area="heap",__SEL__}) or sum by (instance) (elasticsearch_jvm_memory_used_bytes{area="heap",__SEL__})`},
			{Key: "es_search_ts", Title: "Search Query Rate", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(elasticsearch_indices_search_query_total{__SEL__}[5m]))`},
		},
	},
	{
		Name: "db_stats", Title: "Indices / Disk",
		Panels: []PanelDef{
			{Key: "es_index_size", Title: "Index Store Size", Unit: "bytes", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"index"},
				PromQL: `topk(15, sum by (index) (elasticsearch_indices_store_size_bytes_primary{__SEL__} or elasticsearch_indices_store_size_bytes{__SEL__}))`},
			{Key: "es_disk", Title: "Filesystem Available", Unit: "bytes", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"name", "instance"},
				PromQL: `elasticsearch_filesystem_data_available_bytes{__SEL__}`},
		},
	},
	{
		Name: "sessions", Title: "Indexing / Pending",
		Panels: []PanelDef{
			{Key: "es_index_rate", Title: "Indexing Rate", Unit: "/s", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `sum by (instance) (rate(elasticsearch_indices_indexing_index_total{__SEL__}[5m]))`},
			{Key: "es_pending", Title: "Pending Tasks", Unit: "num", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"cluster"},
				PromQL: `elasticsearch_cluster_health_number_of_pending_tasks{__SEL__}`},
		},
	},
}
