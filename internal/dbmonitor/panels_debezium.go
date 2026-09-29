package dbmonitor

// Debezium 对齐官方 debezium-dashboard.json（JMX exporter：debezium_metrics_*）。
var debeziumSections = []SectionDef{
	{
		Name: "overview", Title: "Streaming",
		Panels: []PanelDef{
			{Key: "deb_connected", Title: "Connected", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(debezium_metrics_Connected{context=~"(binlog|streaming)",__SEL__})`},
			{Key: "deb_lag_ms", Title: "ms Since Last Event", Unit: "ms", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(debezium_metrics_MilliSecondsSinceLastEvent{context=~"(binlog|streaming)",__SEL__})`},
			{Key: "deb_events", Title: "Events Seen", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(debezium_metrics_TotalNumberOfEventsSeen{context=~"(binlog|streaming)",__SEL__})`},
			{Key: "deb_skipped", Title: "Events Skipped", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(debezium_metrics_NumberOfEventsSkipped{context=~"(binlog|streaming)",__SEL__})`},

			{Key: "deb_lag_ts", Title: "MilliSeconds Since Last Event", Unit: "ms", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"name", "plugin"},
				PromQL: `debezium_metrics_MilliSecondsSinceLastEvent{context=~"(binlog|streaming)",__SEL__}`},
			{Key: "deb_events_ts", Title: "Total Events Seen", Unit: "num", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"name"},
				PromQL: `debezium_metrics_TotalNumberOfEventsSeen{context=~"(binlog|streaming)",__SEL__}`},
			{Key: "deb_queue_ts", Title: "Queue Remaining Capacity", Unit: "num", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"name"},
				PromQL: `debezium_metrics_QueueRemainingCapacity{__SEL__} or debezium_metrics_QueueTotalCapacity{__SEL__}`},
		},
	},
	{
		Name: "db_stats", Title: "Snapshot",
		Panels: []PanelDef{
			{Key: "deb_snap_tables", Title: "Snapshot Tables Total/Remaining", Unit: "num", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(debezium_metrics_TotalTableCount{context="snapshot",__SEL__}, "kind", "total", "", "") or label_replace(debezium_metrics_RemainingTableCount{context="snapshot",__SEL__}, "kind", "remaining", "", "")`},
			{Key: "deb_snap_flags", Title: "Snapshot Running/Completed/Aborted", Unit: "num", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(debezium_metrics_SnapshotRunning{context="snapshot",__SEL__}, "kind", "running", "", "") or label_replace(debezium_metrics_SnapshotCompleted{context="snapshot",__SEL__}, "kind", "completed", "", "") or label_replace(debezium_metrics_SnapshotAborted{context="snapshot",__SEL__}, "kind", "aborted", "", "")`},
			{Key: "deb_rows_scanned", Title: "Rows Scanned", Unit: "rows", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"table", "name"},
				PromQL: `topk(20, debezium_metrics_RowsScanned{__SEL__})`},
		},
	},
	{
		Name: "sessions", Title: "Connect / JVM",
		Panels: []PanelDef{
			{Key: "deb_connect_net", Title: "Kafka Connect Byte Rate", Unit: "bytes/s", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(kafka_connect_metrics_incoming_byte_rate{__SEL__}, "kind", "in", "", "") or label_replace(kafka_connect_metrics_outgoing_byte_rate{__SEL__}, "kind", "out", "", "")`},
			{Key: "deb_jvm_heap", Title: "JVM Heap Used", Unit: "bytes", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `jvm_memory_bytes_used{area="heap",__SEL__}`},
			{Key: "deb_jvm_threads", Title: "JVM Threads", Unit: "num", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(jvm_threads_current{__SEL__}, "kind", "current", "", "") or label_replace(jvm_threads_peak{__SEL__}, "kind", "peak", "", "")`},
		},
	},
}
