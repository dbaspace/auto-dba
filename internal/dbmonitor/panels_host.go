package dbmonitor

// Host 对齐 Grafana Node Exporter Full（1860 / rfmoz）：node_*。
var hostSections = []SectionDef{
	{
		Name: "overview", Title: "Overview",
		Panels: []PanelDef{
			{Key: "host_uptime", Title: "Uptime", Unit: "s", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(node_time_seconds{__SEL__} - node_boot_time_seconds{__SEL__})`},
			{Key: "host_cpu_cores", Title: "CPU Cores", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `count(count(node_cpu_seconds_total{__SEL__}) by (cpu))`},
			{Key: "host_cpu_util", Title: "CPU Usage", Unit: "percent", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `100 * (1 - avg(rate(node_cpu_seconds_total{mode="idle",__SEL__}[5m])))`},
			{Key: "host_mem_util", Title: "Memory Usage", Unit: "percent", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `clamp_min((1 - (node_memory_MemAvailable_bytes{__SEL__} / node_memory_MemTotal_bytes{__SEL__})) * 100, 0)`},
			{Key: "host_root_util", Title: "Root FS Usage", Unit: "percent", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `((node_filesystem_size_bytes{mountpoint="/",fstype!="rootfs",__SEL__} - node_filesystem_avail_bytes{mountpoint="/",fstype!="rootfs",__SEL__}) / clamp_min(node_filesystem_size_bytes{mountpoint="/",fstype!="rootfs",__SEL__}, 1)) * 100`},
			{Key: "host_load1", Title: "Load 1m", Unit: "num", Section: "overview", Kind: KindStat, Scope: ScopeDB,
				PromQL: `max(node_load1{__SEL__})`},

			{Key: "host_cpu_ts", Title: "CPU Usage by Mode", Unit: "ratio", Section: "overview", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"mode"},
				PromQL: `sum by (mode) (irate(node_cpu_seconds_total{__SEL__}[5m])) / clamp_min(count(count(node_cpu_seconds_total{__SEL__}) by (cpu)), 1)`},
			{Key: "host_load_ts", Title: "System Load", Unit: "num", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(node_load1{__SEL__}, "kind", "load1", "", "") or label_replace(node_load5{__SEL__}, "kind", "load5", "", "") or label_replace(node_load15{__SEL__}, "kind", "load15", "", "")`},
			{Key: "host_mem_ts", Title: "Memory", Unit: "bytes", Section: "overview", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(node_memory_MemTotal_bytes{__SEL__}, "kind", "total", "", "") or label_replace(node_memory_MemAvailable_bytes{__SEL__}, "kind", "available", "", "") or label_replace(node_memory_MemTotal_bytes{__SEL__} - node_memory_MemAvailable_bytes{__SEL__}, "kind", "used", "", "")`},
		},
	},
	{
		Name: "db_stats", Title: "Disk / Network",
		Panels: []PanelDef{
			{Key: "host_fs_util", Title: "Filesystem Usage %", Unit: "percent", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"mountpoint", "device"},
				PromQL: `((node_filesystem_size_bytes{device!~"rootfs",__SEL__} - node_filesystem_avail_bytes{device!~"rootfs",__SEL__}) / clamp_min(node_filesystem_size_bytes{device!~"rootfs",__SEL__}, 1)) * 100`},
			{Key: "host_disk_io", Title: "Disk I/O Bytes / sec", Unit: "bytes/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind", "device"},
				PromQL: `label_replace(sum by (device) (rate(node_disk_read_bytes_total{__SEL__}[5m])), "kind", "read", "", "") or label_replace(sum by (device) (rate(node_disk_written_bytes_total{__SEL__}[5m])), "kind", "write", "", "")`},
			{Key: "host_net_ts", Title: "Network Bandwidth", Unit: "bytes/s", Section: "db_stats", Kind: KindTimeseries, Span: "full",
				Scope: ScopeDB, LegendKeys: []string{"kind", "device"},
				PromQL: `label_replace(rate(node_network_receive_bytes_total{device!~"lo|veth.*|docker.*|br.*|cni.*|flannel.*",__SEL__}[5m]), "kind", "receive", "", "") or label_replace(rate(node_network_transmit_bytes_total{device!~"lo|veth.*|docker.*|br.*|cni.*|flannel.*",__SEL__}[5m]), "kind", "transmit", "", "")`},
		},
	},
	{
		Name: "sessions", Title: "Pressure / Swap",
		Panels: []PanelDef{
			{Key: "host_swap", Title: "Swap Used", Unit: "bytes", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(node_memory_SwapTotal_bytes{__SEL__} - node_memory_SwapFree_bytes{__SEL__}, "kind", "used", "", "") or label_replace(node_memory_SwapTotal_bytes{__SEL__}, "kind", "total", "", "")`},
			{Key: "host_pressure", Title: "PSI Waiting", Unit: "/s", Section: "sessions", Kind: KindTimeseries, Span: "half",
				Scope: ScopeDB, LegendKeys: []string{"kind"},
				PromQL: `label_replace(irate(node_pressure_cpu_waiting_seconds_total{__SEL__}[5m]), "kind", "cpu", "", "") or label_replace(irate(node_pressure_memory_waiting_seconds_total{__SEL__}[5m]), "kind", "memory", "", "") or label_replace(irate(node_pressure_io_waiting_seconds_total{__SEL__}[5m]), "kind", "io", "", "")`},
		},
	},
}
