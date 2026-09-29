package dbmonitor

// Kafka（Grafana uid M1CD8_2Wc）。
var kafkaSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "kafka_brokers", Title: "Broker", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(kafka_brokers{__SEL__})`},
			{Key: "kafka_partitions", Title: "Partitions", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(kafka_topic_partitions{__SEL__})`},
			{Key: "kafka_produce", Title: "topic_partition_current_offset(生产速率)", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"topic"},
				PromQL: `topk(20, sum by (topic) (rate(kafka_topic_partition_current_offset{__SEL__}[5m])))`},
			{Key: "kafka_consume", Title: "consumergroup_current_offset(消费速率)", Unit: "/s", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"topic", "consumergroup"},
				PromQL: `topk(10, sum by (topic, consumergroup) (rate(kafka_consumergroup_current_offset{__SEL__}[5m])))`},
		},
	},
	{
		Name: "db_stats", Title: "Lag / Partitions",
		Panels: []PanelDef{
			{Key: "kafka_lag", Title: "consumergroup_lag(消息积压)", Unit: "msgs", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"topic"},
				PromQL: `topk(10, sum by (topic) (kafka_consumergroup_lag{__SEL__}))`},
			{Key: "kafka_parts_topic", Title: "Partitions per Topic", Unit: "num", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"topic"},
				PromQL: `topk(10, sum by (topic) (kafka_topic_partitions{__SEL__}))`},
		},
	},
	{
		Name: "sessions", Title: "Extra",
		Panels: []PanelDef{
			{Key: "kafka_lag_group", Title: "Lag by consumergroup", Unit: "msgs", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"consumergroup", "topic"},
				PromQL: `topk(10, sum by (consumergroup, topic) (kafka_consumergroup_lag{__SEL__}))`},
		},
	},
}
