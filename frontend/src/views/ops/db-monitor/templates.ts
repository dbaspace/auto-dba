/** 各引擎监控固定模版（与后端 panels_* 对齐） */

export type TemplatePanel = {
  key: string
  title: string
  unit: string
  kind: 'stat' | 'timeseries'
  span?: 'half' | 'full'
}

export type TemplateSection = {
  key: string
  title: string
  stats: TemplatePanel[]
  charts: TemplatePanel[]
}

export type MonitorEngine =
  | 'postgres'
  | 'mysql'
  | 'redis'
  | 'starrocks'
  | 'clickhouse'
  | 'kafka'
  | 'mongodb'
  | 'oracle'
  | 'elasticsearch'
  | 'sqlserver'
  | 'tidb'
  | 'cockroachdb'
  | 'cassandra'
  | 'oceanbase'
  | 'host'
  | 'debezium'
  | 'dolphinscheduler'

export const PG_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'pg_version', title: 'Version', unit: 'text', kind: 'stat' },
      { key: 'pg_start_time', title: 'Start Time', unit: 'unix', kind: 'stat' },
      { key: 'fetch_data', title: 'Current fetch data', unit: 'rows', kind: 'stat' },
      { key: 'insert_data', title: 'Current insert data', unit: 'rows', kind: 'stat' },
      { key: 'update_data', title: 'Current update data', unit: 'rows', kind: 'stat' },
      { key: 'max_connections', title: 'Max Connections', unit: 'conns', kind: 'stat' },
    ],
    charts: [
      { key: 'cpu_usage', title: 'CPU使用率', unit: 'ratio', kind: 'timeseries', span: 'half' },
      { key: 'mem_usage', title: 'Memory使用率', unit: 'ratio', kind: 'timeseries', span: 'half' },
      { key: 'disk_bytes', title: '磁盘用量', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'disk_usage', title: '磁盘使用率', unit: 'ratio', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'settings',
    title: 'Settings',
    stats: [
      { key: 'shared_buffers', title: 'Shared Buffers', unit: 'bytes', kind: 'stat' },
      { key: 'effective_cache', title: 'Effective Cache', unit: 'bytes', kind: 'stat' },
      { key: 'maintenance_work_mem', title: 'Maintenance Work Mem', unit: 'bytes', kind: 'stat' },
      { key: 'work_mem', title: 'Work Mem', unit: 'bytes', kind: 'stat' },
      { key: 'max_wal_size', title: 'Max WAL Size', unit: 'bytes', kind: 'stat' },
      { key: 'random_page_cost', title: 'Random Page Cost', unit: 'num', kind: 'stat' },
      { key: 'seq_page_cost', title: 'Seq Page Cost', unit: 'num', kind: 'stat' },
      { key: 'max_worker_processes', title: 'Max Worker Processes', unit: 'num', kind: 'stat' },
      { key: 'max_parallel_workers', title: 'Max Parallel Workers', unit: 'num', kind: 'stat' },
    ],
    charts: [],
  },
  {
    key: 'db_stats',
    title: 'Database Stats',
    stats: [],
    charts: [
      { key: 'fetched_ps', title: '每秒fetched数量', unit: 'rows/s', kind: 'timeseries', span: 'full' },
      { key: 'returned_ps', title: 'Number of rows returned', unit: 'rows/s', kind: 'timeseries', span: 'full' },
      { key: 'transactions', title: 'Transactions', unit: 'tx/s', kind: 'timeseries', span: 'full' },
      { key: 'cache_hit', title: 'Cache Hit Rate', unit: 'ratio', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Sessions / Locks / Temp',
    stats: [],
    charts: [
      { key: 'active_sessions', title: 'Active sessions', unit: 'conns', kind: 'timeseries', span: 'half' },
      { key: 'idle_sessions', title: 'Idle sessions', unit: 'conns', kind: 'timeseries', span: 'half' },
      { key: 'lock_tables', title: 'Lock tables', unit: 'locks', kind: 'timeseries', span: 'full' },
      { key: 'temp_bytes', title: 'Temp File (Bytes)', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'deadlocks', title: 'Conflicts/Deadlocks', unit: '/s', kind: 'timeseries', span: 'half' },
    ],
  },
]

export const MYSQL_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'mysql_uptime', title: 'Uptime', unit: 'seconds', kind: 'stat' },
      { key: 'mysql_qps', title: 'Current QPS', unit: 'qps', kind: 'stat' },
      { key: 'mysql_buffer_pool', title: 'InnoDB Buffer Pool', unit: 'bytes', kind: 'stat' },
      { key: 'mysql_threads', title: 'Threads Connected', unit: 'conns', kind: 'stat' },
      { key: 'mysql_slow', title: 'Slow Queries', unit: 'num', kind: 'stat' },
      { key: 'mysql_max_conn', title: 'Max Connections', unit: 'conns', kind: 'stat' },
    ],
    charts: [
      { key: 'mysql_conn_ts', title: 'MySQL Connections', unit: 'conns', kind: 'timeseries', span: 'half' },
      { key: 'mysql_questions_ts', title: 'MySQL Questions', unit: 'qps', kind: 'timeseries', span: 'half' },
      { key: 'mysql_net_ts', title: 'MySQL Network Traffic', unit: 'bytes/s', kind: 'timeseries', span: 'half' },
      { key: 'mysql_slow_ts', title: 'MySQL Slow Queries', unit: '/s', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'settings',
    title: 'Cache / Files',
    stats: [
      { key: 'mysql_thread_cache', title: 'Thread Cache Size', unit: 'num', kind: 'stat' },
      { key: 'mysql_open_files', title: 'Open Files', unit: 'num', kind: 'stat' },
      { key: 'mysql_open_tables', title: 'Open Tables', unit: 'num', kind: 'stat' },
      { key: 'mysql_table_def', title: 'Open Table Definitions', unit: 'num', kind: 'stat' },
    ],
    charts: [],
  },
  {
    key: 'db_stats',
    title: 'Database Stats',
    stats: [],
    charts: [
      { key: 'mysql_tmp', title: 'MySQL Temporary Objects', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'mysql_select_types', title: 'MySQL Select Types', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'mysql_sorts', title: 'MySQL Sorts', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'mysql_handlers', title: 'MySQL Handlers', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Process / Locks',
    stats: [],
    charts: [
      { key: 'mysql_process', title: 'Process States', unit: 'threads', kind: 'timeseries', span: 'full' },
      { key: 'mysql_table_locks', title: 'MySQL Table Locks', unit: '/s', kind: 'timeseries', span: 'half' },
      { key: 'mysql_aborted', title: 'Aborted Connections', unit: '/s', kind: 'timeseries', span: 'half' },
    ],
  },
]

export const REDIS_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'redis_uptime', title: 'Uptime', unit: 'seconds', kind: 'stat' },
      { key: 'redis_qps', title: 'QPS', unit: 'ops', kind: 'stat' },
      { key: 'redis_mem_max', title: '最大内存', unit: 'bytes', kind: 'stat' },
      { key: 'redis_mem_util', title: 'Memory Usage', unit: 'percent', kind: 'stat' },
    ],
    charts: [
      { key: 'redis_hits', title: 'Hits / Misses per Sec', unit: '/s', kind: 'timeseries', span: 'half' },
      { key: 'redis_commands', title: 'Commands Executed / sec', unit: '/s', kind: 'timeseries', span: 'half' },
      { key: 'redis_net', title: 'Network I/O', unit: 'bytes/s', kind: 'timeseries', span: 'half' },
      { key: 'redis_mem_ts', title: 'Total Memory Usage', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'redis_clients', title: 'Clients', unit: 'conns', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Keys & Expiry',
    stats: [],
    charts: [
      { key: 'redis_db_keys', title: 'Total Items per DB', unit: 'short', kind: 'timeseries', span: 'full' },
      { key: 'redis_expiry', title: 'Expiring vs Not-Expiring Keys', unit: 'short', kind: 'timeseries', span: 'full' },
      { key: 'redis_expired', title: 'Expired / Evicted', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const STARROCKS_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'sr_fe_nodes', title: 'FE Node', unit: 'num', kind: 'stat' },
      { key: 'sr_be_nodes', title: 'BE Node', unit: 'num', kind: 'stat' },
      { key: 'sr_be_alive', title: 'BE Alive', unit: 'num', kind: 'stat' },
      { key: 'sr_used_cap', title: 'Used Capacity', unit: 'bytes', kind: 'stat' },
      { key: 'sr_total_cap', title: 'Total Capacity', unit: 'bytes', kind: 'stat' },
      { key: 'sr_db_count', title: 'Database Count', unit: 'num', kind: 'stat' },
    ],
    charts: [
      { key: 'sr_qps', title: 'Cluster QPS', unit: 'qps', kind: 'timeseries', span: 'half' },
      { key: 'sr_rps', title: 'Cluster RPS', unit: 'rps', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'cluster_overview',
    title: 'Cluster Overview',
    stats: [
      { key: 'sr_table_count', title: 'Table Count', unit: 'num', kind: 'stat' },
      { key: 'sr_tablet_count', title: 'Tablet Count', unit: 'num', kind: 'stat' },
    ],
    charts: [
      { key: 'sr_be_cpu', title: 'Cluster BE CPU Idle', unit: 'ratio', kind: 'timeseries', span: 'half' },
      { key: 'sr_be_mem', title: 'Cluster BE Mem', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'sr_tablet_ts', title: 'Total Tablet', unit: 'num', kind: 'timeseries', span: 'half' },
      { key: 'sr_compaction', title: 'BE Max Compaction Score', unit: 'num', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'query_statistic',
    title: 'Query Statistic',
    stats: [],
    charts: [
      { key: 'sr_query_err', title: 'Cluster Query Error', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'sr_tps', title: 'Cluster TPS', unit: 'tps', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'fe_jvm',
    title: 'FE JVM',
    stats: [{ key: 'sr_fe_jvm', title: 'FE JVM Heap Used %', unit: 'percent', kind: 'stat' }],
    charts: [
      { key: 'sr_fe_jvm_used', title: 'FE JVM Heap Used', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'sr_fe_jvm_max', title: 'FE JVM Heap Max', unit: 'bytes', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'be',
    title: 'BE',
    stats: [],
    charts: [
      { key: 'sr_disk_io', title: 'BE IO Util', unit: 'percent', kind: 'timeseries', span: 'half' },
      { key: 'sr_disk_state', title: 'Disk State', unit: 'num', kind: 'timeseries', span: 'half' },
      { key: 'sr_be_mem_inst', title: 'BE Process Mem', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'sr_be_disk_used', title: 'BE Disk Used', unit: 'bytes', kind: 'timeseries', span: 'half' },
    ],
  },
]

export const CLICKHOUSE_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'ch_uptime', title: 'Uptime', unit: 'seconds', kind: 'stat' },
      { key: 'ch_bytes', title: 'TotalBytesOfMergeTreeTables', unit: 'bytes', kind: 'stat' },
      { key: 'ch_rows', title: 'TotalRowsOfMergeTreeTables', unit: 'rows', kind: 'stat' },
    ],
    charts: [
      { key: 'ch_cpu', title: 'IdleTimeCPU (busy)', unit: 'ratio', kind: 'timeseries', span: 'half' },
      { key: 'ch_space', title: '空间占用情况', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'ch_rows_ts', title: '数据行趋势', unit: 'rows', kind: 'timeseries', span: 'half' },
      { key: 'ch_fail', title: '读取写入失败', unit: '/s', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'settings',
    title: 'Disk',
    stats: [
      { key: 'ch_disk_hot', title: '热盘使用情况', unit: 'bytes', kind: 'stat' },
      { key: 'ch_disk_root', title: '根盘使用情况', unit: 'bytes', kind: 'stat' },
    ],
    charts: [],
  },
  {
    key: 'db_stats',
    title: 'Query / Insert',
    stats: [],
    charts: [
      { key: 'ch_query', title: 'TotalQuery/M', unit: '/m', kind: 'timeseries', span: 'full' },
      { key: 'ch_select', title: 'SelectQuery/M', unit: '/m', kind: 'timeseries', span: 'full' },
      { key: 'ch_insert', title: 'InsertNumbers/M', unit: '/m', kind: 'timeseries', span: 'full' },
      { key: 'ch_inserted_rows', title: 'InsertedRows/M', unit: 'rows/m', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Merge / Cache',
    stats: [],
    charts: [
      { key: 'ch_merge', title: 'Merge numbers', unit: '/m', kind: 'timeseries', span: 'half' },
      { key: 'ch_merge_rows', title: 'Merge rows', unit: 'rows/m', kind: 'timeseries', span: 'half' },
      { key: 'ch_mark_cache', title: 'MarkCache Misses/Hits', unit: '/m', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const KAFKA_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'kafka_brokers', title: 'Broker', unit: 'num', kind: 'stat' },
      { key: 'kafka_partitions', title: 'Partitions', unit: 'num', kind: 'stat' },
    ],
    charts: [
      { key: 'kafka_produce', title: 'topic_partition_current_offset(生产速率)', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'kafka_consume', title: 'consumergroup_current_offset(消费速率)', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Lag / Partitions',
    stats: [],
    charts: [
      { key: 'kafka_lag', title: 'consumergroup_lag(消息积压)', unit: 'msgs', kind: 'timeseries', span: 'full' },
      { key: 'kafka_parts_topic', title: 'Partitions per Topic', unit: 'num', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Extra',
    stats: [],
    charts: [
      { key: 'kafka_lag_group', title: 'Lag by consumergroup', unit: 'msgs', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const MONGODB_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'mongo_up', title: 'Up', unit: 'num', kind: 'stat' },
      { key: 'mongo_conns', title: 'Connections', unit: 'conns', kind: 'stat' },
      { key: 'mongo_mem', title: 'Resident Memory', unit: 'bytes', kind: 'stat' },
      { key: 'mongo_ops', title: 'Opcounters/s', unit: 'ops', kind: 'stat' },
    ],
    charts: [
      { key: 'mongo_ops_ts', title: 'Operations / sec', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'mongo_net_ts', title: 'Network I/O', unit: 'bytes/s', kind: 'timeseries', span: 'half' },
      { key: 'mongo_conn_ts', title: 'Connections', unit: 'conns', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Replication / WiredTiger',
    stats: [],
    charts: [
      { key: 'mongo_repl_lag', title: 'Replication Lag', unit: 's', kind: 'timeseries', span: 'full' },
      { key: 'mongo_wt_cache', title: 'WiredTiger Cache Used', unit: 'bytes', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Asserts / Queues',
    stats: [],
    charts: [
      { key: 'mongo_asserts', title: 'Asserts / sec', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'mongo_queued', title: 'Queued Readers/Writers', unit: 'num', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const ORACLE_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'oracle_up', title: 'Up', unit: 'num', kind: 'stat' },
      { key: 'oracle_sessions', title: 'Active Sessions', unit: 'conns', kind: 'stat' },
      { key: 'oracle_process', title: 'Processes', unit: 'num', kind: 'stat' },
      { key: 'oracle_exec', title: 'Execute Count/s', unit: 'ops', kind: 'stat' },
    ],
    charts: [
      { key: 'oracle_activity_ts', title: 'Activity Rates', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'oracle_sessions_ts', title: 'Sessions by Status', unit: 'conns', kind: 'timeseries', span: 'half' },
      { key: 'oracle_wait_ts', title: 'Wait Time', unit: 's', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Tablespace',
    stats: [],
    charts: [
      { key: 'oracle_tbs_used', title: 'Tablespace Used Bytes', unit: 'bytes', kind: 'timeseries', span: 'full' },
      { key: 'oracle_tbs_free', title: 'Tablespace Free Bytes', unit: 'bytes', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Resource',
    stats: [],
    charts: [
      { key: 'oracle_resource', title: 'Resource Limit Current', unit: 'num', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const ELASTICSEARCH_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'es_nodes', title: 'Nodes', unit: 'num', kind: 'stat' },
      { key: 'es_data_nodes', title: 'Data Nodes', unit: 'num', kind: 'stat' },
      { key: 'es_shards', title: 'Active Shards', unit: 'num', kind: 'stat' },
      { key: 'es_docs', title: 'Docs', unit: 'num', kind: 'stat' },
    ],
    charts: [
      { key: 'es_health_ts', title: 'Cluster Health Status', unit: 'num', kind: 'timeseries', span: 'half' },
      { key: 'es_jvm_ts', title: 'JVM Heap Used', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'es_search_ts', title: 'Search Query Rate', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Indices / Disk',
    stats: [],
    charts: [
      { key: 'es_index_size', title: 'Index Store Size', unit: 'bytes', kind: 'timeseries', span: 'full' },
      { key: 'es_disk', title: 'Filesystem Available', unit: 'bytes', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Indexing / Pending',
    stats: [],
    charts: [
      { key: 'es_index_rate', title: 'Indexing Rate', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'es_pending', title: 'Pending Tasks', unit: 'num', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const SQLSERVER_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'mssql_up', title: 'Up', unit: 'num', kind: 'stat' },
      { key: 'mssql_conns', title: 'Connections', unit: 'conns', kind: 'stat' },
      { key: 'mssql_batch', title: 'Batch Requests/s', unit: 'ops', kind: 'stat' },
      { key: 'mssql_ple', title: 'Page Life Expectancy', unit: 's', kind: 'stat' },
    ],
    charts: [
      { key: 'mssql_batch_ts', title: 'Batch Requests / sec', unit: '/s', kind: 'timeseries', span: 'half' },
      { key: 'mssql_conn_ts', title: 'Connections', unit: 'conns', kind: 'timeseries', span: 'half' },
      { key: 'mssql_deadlock_ts', title: 'Deadlocks / sec', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Buffer / IO',
    stats: [],
    charts: [
      { key: 'mssql_buffer_hit', title: 'Buffer Cache Hit Ratio', unit: 'percent', kind: 'timeseries', span: 'full' },
      { key: 'mssql_io', title: 'Page Reads/Writes / sec', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Waits',
    stats: [],
    charts: [
      { key: 'mssql_waits', title: 'Wait Stats / sec', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const DEBEZIUM_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Streaming',
    stats: [
      { key: 'deb_connected', title: 'Connected', unit: 'num', kind: 'stat' },
      { key: 'deb_lag_ms', title: 'ms Since Last Event', unit: 'ms', kind: 'stat' },
      { key: 'deb_events', title: 'Events Seen', unit: 'num', kind: 'stat' },
      { key: 'deb_skipped', title: 'Events Skipped', unit: 'num', kind: 'stat' },
    ],
    charts: [
      { key: 'deb_lag_ts', title: 'MilliSeconds Since Last Event', unit: 'ms', kind: 'timeseries', span: 'full' },
      { key: 'deb_events_ts', title: 'Total Events Seen', unit: 'num', kind: 'timeseries', span: 'half' },
      { key: 'deb_queue_ts', title: 'Queue Remaining Capacity', unit: 'num', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Snapshot',
    stats: [],
    charts: [
      { key: 'deb_snap_tables', title: 'Snapshot Tables Total/Remaining', unit: 'num', kind: 'timeseries', span: 'full' },
      { key: 'deb_snap_flags', title: 'Snapshot Running/Completed/Aborted', unit: 'num', kind: 'timeseries', span: 'full' },
      { key: 'deb_rows_scanned', title: 'Rows Scanned', unit: 'rows', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Connect / JVM',
    stats: [],
    charts: [
      { key: 'deb_connect_net', title: 'Kafka Connect Byte Rate', unit: 'bytes/s', kind: 'timeseries', span: 'full' },
      { key: 'deb_jvm_heap', title: 'JVM Heap Used', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'deb_jvm_threads', title: 'JVM Threads', unit: 'num', kind: 'timeseries', span: 'half' },
    ],
  },
]

export const DOLPHIN_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Master Overview',
    stats: [
      { key: 'ds_job_total', title: 'Quartz Jobs Total', unit: 'num', kind: 'stat' },
      { key: 'ds_job_success_rate', title: 'Job Success Rate', unit: 'percent', kind: 'stat' },
      { key: 'ds_overload', title: 'Master Overload/1m', unit: 'num', kind: 'stat' },
      { key: 'ds_consume_cmd', title: 'Consume Command/1m', unit: 'num', kind: 'stat' },
    ],
    charts: [
      { key: 'ds_job_exec_ts', title: 'Quartz Job Executed / min', unit: '/m', kind: 'timeseries', span: 'full' },
      { key: 'ds_wf_state_ts', title: 'Workflow Instance State / min', unit: '/m', kind: 'timeseries', span: 'full' },
      { key: 'ds_jvm_threads', title: 'JVM Threads', unit: 'num', kind: 'timeseries', span: 'half' },
      { key: 'ds_jvm_mem', title: 'JVM Memory Used', unit: 'bytes', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Task Dispatch',
    stats: [],
    charts: [
      { key: 'ds_task_dispatch', title: 'Task Dispatch / min', unit: '/m', kind: 'timeseries', span: 'full' },
      { key: 'ds_task_state', title: 'Task Instance State / min', unit: '/m', kind: 'timeseries', span: 'full' },
      { key: 'ds_job_latency', title: 'Quartz Job Execution Time p95', unit: 's', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Worker',
    stats: [],
    charts: [
      { key: 'ds_worker_exec', title: 'Worker Execute / min', unit: '/m', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const TIDB_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'tidb_conns', title: 'Connections', unit: 'conns', kind: 'stat' },
      { key: 'tidb_qps', title: 'QPS', unit: 'qps', kind: 'stat' },
      { key: 'tidb_latency_p99', title: 'Query Duration p99', unit: 's', kind: 'stat' },
      { key: 'tidb_err', title: 'Execute Errors/s', unit: '/s', kind: 'stat' },
    ],
    charts: [
      { key: 'tidb_stmt_ts', title: 'Statements / sec', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'tidb_conn_ts', title: 'Connections', unit: 'conns', kind: 'timeseries', span: 'half' },
      { key: 'tidb_latency_ts', title: 'Query Duration p95/p99', unit: 's', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Transaction / TiKV Client',
    stats: [],
    charts: [
      { key: 'tidb_txn_ts', title: 'Transaction Duration', unit: 's', kind: 'timeseries', span: 'full' },
      { key: 'tidb_tikv_backoff', title: 'TiKV Client Backoff', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Runtime',
    stats: [],
    charts: [
      { key: 'tidb_mem_ts', title: 'Process Memory', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'tidb_goroutines', title: 'Goroutines', unit: 'num', kind: 'timeseries', span: 'half' },
    ],
  },
]

export const COCKROACH_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'crdb_uptime', title: 'Uptime', unit: 's', kind: 'stat' },
      { key: 'crdb_conns', title: 'SQL Connections', unit: 'conns', kind: 'stat' },
      { key: 'crdb_qps', title: 'SQL Statements/s', unit: 'qps', kind: 'stat' },
      { key: 'crdb_cpu', title: 'CPU Combined %', unit: 'percent', kind: 'stat' },
    ],
    charts: [
      { key: 'crdb_sql_ts', title: 'SQL Statements / sec', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'crdb_conn_ts', title: 'SQL Connections', unit: 'conns', kind: 'timeseries', span: 'half' },
      { key: 'crdb_latency_ts', title: 'SQL Service Latency p99', unit: 's', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Storage / Capacity',
    stats: [],
    charts: [
      { key: 'crdb_capacity', title: 'Capacity Used / Available', unit: 'bytes', kind: 'timeseries', span: 'full' },
      { key: 'crdb_rss', title: 'RSS Memory', unit: 'bytes', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Replication',
    stats: [],
    charts: [
      { key: 'crdb_replicas', title: 'Replicas / Ranges', unit: 'num', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const CASSANDRA_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'cass_up', title: 'Up', unit: 'num', kind: 'stat' },
      { key: 'cass_clients', title: 'Native Clients', unit: 'conns', kind: 'stat' },
      { key: 'cass_load', title: 'Storage Load', unit: 'bytes', kind: 'stat' },
      { key: 'cass_pending', title: 'Compaction Pending', unit: 'num', kind: 'stat' },
    ],
    charts: [
      { key: 'cass_read_lat', title: 'Read Latency', unit: 's', kind: 'timeseries', span: 'half' },
      { key: 'cass_write_lat', title: 'Write Latency', unit: 's', kind: 'timeseries', span: 'half' },
      { key: 'cass_throughput', title: 'Client Requests / sec', unit: '/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Cache / Compaction',
    stats: [],
    charts: [
      { key: 'cass_cache', title: 'Key Cache Hit Rate', unit: 'ratio', kind: 'timeseries', span: 'full' },
      { key: 'cass_compaction_ts', title: 'Compaction Pending Tasks', unit: 'num', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'JVM',
    stats: [],
    charts: [
      { key: 'cass_jvm_heap', title: 'JVM Heap Used', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'cass_jvm_gc', title: 'GC Pause / sec', unit: 's', kind: 'timeseries', span: 'half' },
    ],
  },
]

export const OCEANBASE_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'ob_sessions', title: 'Active Sessions', unit: 'conns', kind: 'stat' },
      { key: 'ob_sql_qps', title: 'SQL QPS', unit: 'qps', kind: 'stat' },
      { key: 'ob_tps', title: 'TPS', unit: 'ops', kind: 'stat' },
      { key: 'ob_trans_rt', title: 'Transaction RT', unit: 'us', kind: 'stat' },
    ],
    charts: [
      { key: 'ob_sql_ts', title: 'SQL QPS by Type', unit: '/s', kind: 'timeseries', span: 'full' },
      { key: 'ob_session_ts', title: 'Active Sessions', unit: 'conns', kind: 'timeseries', span: 'half' },
      { key: 'ob_wait_ts', title: 'Wait Events / sec', unit: '/s', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Cache / IO',
    stats: [],
    charts: [
      { key: 'ob_cache', title: 'Cache Size', unit: 'bytes', kind: 'timeseries', span: 'full' },
      { key: 'ob_plan_cache', title: 'Plan Cache Hit Ratio', unit: 'ratio', kind: 'timeseries', span: 'full' },
      { key: 'ob_io', title: 'IO Bytes / sec', unit: 'bytes/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Memory',
    stats: [],
    charts: [
      { key: 'ob_mem', title: 'Memory Used / Limit', unit: 'bytes', kind: 'timeseries', span: 'full' },
    ],
  },
]

export const HOST_MONITOR_TEMPLATE: TemplateSection[] = [
  {
    key: 'overview',
    title: 'Overview',
    stats: [
      { key: 'host_uptime', title: 'Uptime', unit: 's', kind: 'stat' },
      { key: 'host_cpu_cores', title: 'CPU Cores', unit: 'num', kind: 'stat' },
      { key: 'host_cpu_util', title: 'CPU Usage', unit: 'percent', kind: 'stat' },
      { key: 'host_mem_util', title: 'Memory Usage', unit: 'percent', kind: 'stat' },
      { key: 'host_root_util', title: 'Root FS Usage', unit: 'percent', kind: 'stat' },
      { key: 'host_load1', title: 'Load 1m', unit: 'num', kind: 'stat' },
    ],
    charts: [
      { key: 'host_cpu_ts', title: 'CPU Usage by Mode', unit: 'ratio', kind: 'timeseries', span: 'full' },
      { key: 'host_load_ts', title: 'System Load', unit: 'num', kind: 'timeseries', span: 'half' },
      { key: 'host_mem_ts', title: 'Memory', unit: 'bytes', kind: 'timeseries', span: 'half' },
    ],
  },
  {
    key: 'db_stats',
    title: 'Disk / Network',
    stats: [],
    charts: [
      { key: 'host_fs_util', title: 'Filesystem Usage %', unit: 'percent', kind: 'timeseries', span: 'full' },
      { key: 'host_disk_io', title: 'Disk I/O Bytes / sec', unit: 'bytes/s', kind: 'timeseries', span: 'full' },
      { key: 'host_net_ts', title: 'Network Bandwidth', unit: 'bytes/s', kind: 'timeseries', span: 'full' },
    ],
  },
  {
    key: 'sessions',
    title: 'Pressure / Swap',
    stats: [],
    charts: [
      { key: 'host_swap', title: 'Swap Used', unit: 'bytes', kind: 'timeseries', span: 'half' },
      { key: 'host_pressure', title: 'PSI Waiting', unit: '/s', kind: 'timeseries', span: 'half' },
    ],
  },
]

const BY_ENGINE: Record<MonitorEngine, TemplateSection[]> = {
  postgres: PG_MONITOR_TEMPLATE,
  mysql: MYSQL_MONITOR_TEMPLATE,
  redis: REDIS_MONITOR_TEMPLATE,
  starrocks: STARROCKS_MONITOR_TEMPLATE,
  clickhouse: CLICKHOUSE_MONITOR_TEMPLATE,
  kafka: KAFKA_MONITOR_TEMPLATE,
  mongodb: MONGODB_MONITOR_TEMPLATE,
  oracle: ORACLE_MONITOR_TEMPLATE,
  elasticsearch: ELASTICSEARCH_MONITOR_TEMPLATE,
  sqlserver: SQLSERVER_MONITOR_TEMPLATE,
  tidb: TIDB_MONITOR_TEMPLATE,
  cockroachdb: COCKROACH_MONITOR_TEMPLATE,
  cassandra: CASSANDRA_MONITOR_TEMPLATE,
  oceanbase: OCEANBASE_MONITOR_TEMPLATE,
  host: HOST_MONITOR_TEMPLATE,
  debezium: DEBEZIUM_MONITOR_TEMPLATE,
  dolphinscheduler: DOLPHIN_MONITOR_TEMPLATE,
}

export function monitorTemplate(engine: string): TemplateSection[] {
  const e = String(engine || 'postgres').toLowerCase() as MonitorEngine
  return BY_ENGINE[e] || PG_MONITOR_TEMPLATE
}
