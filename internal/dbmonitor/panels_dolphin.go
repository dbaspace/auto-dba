package dbmonitor

// DolphinScheduler 对齐官方 grafana/DolphinSchedulerMaster.json（Micrometer ds_*）。
var dolphinSections = []SectionDef{
	{
		Name: "overview", Title: "Master Overview",
		Panels: []PanelDef{
			{Key: "ds_job_total", Title: "Quartz Jobs Total", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(ds_master_quartz_job_executed_total{__SEL__})`},
			{Key: "ds_job_success_rate", Title: "Job Success Rate", Unit: "percent", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `100 * sum(ds_master_quartz_job_executed_total{result="success",__SEL__}) / clamp_min(sum(ds_master_quartz_job_executed_total{__SEL__}), 1)`},
			{Key: "ds_overload", Title: "Master Overload/1m", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(increase(ds_master_overload_count_total{__SEL__}[1m]))`},
			{Key: "ds_consume_cmd", Title: "Consume Command/1m", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `sum(increase(ds_master_consume_command_count_total{__SEL__}[1m]))`},

			{Key: "ds_job_exec_ts", Title: "Quartz Job Executed / min", Unit: "/m", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"result"},
				PromQL: `sum by (result) (increase(ds_master_quartz_job_executed_total{__SEL__}[1m]))`},
			{Key: "ds_wf_state_ts", Title: "Workflow Instance State / min", Unit: "/m", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"state"},
				PromQL: `sum by (state) (increase(ds_workflow_instance_count_total{__SEL__}[1m]))`},
			{Key: "ds_jvm_threads", Title: "JVM Threads", Unit: "num", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(jvm_threads_live_threads{__SEL__}, "kind", "live", "", "") or label_replace(jvm_threads_peak_threads{__SEL__}, "kind", "peak", "", "") or label_replace(jvm_threads_daemon_threads{__SEL__}, "kind", "daemon", "", "")`},
			{Key: "ds_jvm_mem", Title: "JVM Memory Used", Unit: "bytes", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"area"},
				PromQL: `jvm_memory_used_bytes{__SEL__}`},
		},
	},
	{
		Name: "db_stats", Title: "Task Dispatch",
		Panels: []PanelDef{
			{Key: "ds_task_dispatch", Title: "Task Dispatch / min", Unit: "/m", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(sum(increase(ds_task_dispatch_count_total{__SEL__}[1m])), "kind", "ok", "", "") or label_replace(sum(increase(ds_task_dispatch_failure_count_total{__SEL__}[1m])), "kind", "failure", "", "") or label_replace(sum(increase(ds_task_dispatch_error_count_total{__SEL__}[1m])), "kind", "error", "", "")`},
			{Key: "ds_task_state", Title: "Task Instance State / min", Unit: "/m", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"state"},
				PromQL: `sum by (state) (increase(ds_task_instance_count_total{__SEL__}[1m]))`},
			{Key: "ds_job_latency", Title: "Quartz Job Execution Time p95", Unit: "s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"instance"},
				PromQL: `histogram_quantile(0.95, sum by (le) (rate(ds_master_quartz_job_execution_time_seconds_bucket{__SEL__}[5m])))`},
		},
	},
	{
		Name: "sessions", Title: "Worker",
		Panels: []PanelDef{
			{Key: "ds_worker_exec", Title: "Worker Execute / min", Unit: "/m", Section: "sessions", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"state"},
				PromQL: `sum by (state) (increase(ds_worker_task_execute_count_total{__SEL__}[1m]) or increase(ds_task_instance_count_total{__SEL__}[1m]))`},
		},
	},
}
