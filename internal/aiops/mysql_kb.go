package aiops

// MySQLKnowledgeCases MySQL 运维知识库。
// 主题提炼自业界公开排查共识（连接打满、主从延迟、死锁、慢 SQL、临时表、binlog 等），文案原创。
func MySQLKnowledgeCases() []KnowledgeCase {
	return []KnowledgeCase{
		{
			Fingerprint: "conn_usage_ratio|warning|above|mysql", MetricKey: "conn_usage_ratio",
			Severity: "warning", Direction: "above", Title: "MySQL 连接使用率偏高",
			RootCauseTemplate: "连接使用率 {value} 偏高：常见于连接池过大/泄漏、大量 Sleep、慢 SQL 占槽、wait_timeout 过长；打满后报 Too many connections。",
			ActionTemplate: "1) SHOW STATUS LIKE 'Threads_connected' 对照 max_connections。" +
				"2) SHOW FULL PROCESSLIST 按 Command/Time/User 分组，优先处理长时间 Sleep 与Executing。" +
				"3) 核对应用池 maxActive、空闲回收；设合理 wait_timeout/interactive_timeout。" +
				"4) 勿只靠调大 max_connections；留足管理员连接（或额外账号）以便抢救。",
			AlertNames: []string{"MySQLConn"}, Engine: "mysql",
			Tags: []string{"connection", "too many connections", "打满", "processlist"},
		},
		{
			Fingerprint: "conn_usage_ratio|critical|above|mysql", MetricKey: "conn_usage_ratio",
			Severity: "critical", Direction: "above", Title: "MySQL 连接接近打满",
			RootCauseTemplate: "连接使用率 {value} 危急：新连接将被拒绝，业务大面积失败。",
			ActionTemplate: "紧急 kill 空闲过久会话；限流应用新建连接；修泄漏与慢 SQL；评估只读拆分；事后加 Threads_connected 告警。",
			AlertNames: []string{"MySQLConn"}, Engine: "mysql",
			Tags: []string{"connection", "incident", "打满"},
		},
		{
			Fingerprint: "backends|warning|above|mysql", MetricKey: "backends",
			Severity: "warning", Direction: "above", Title: "MySQL Threads_running 偏高",
			RootCauseTemplate: "活动线程 {value} 偏高：多为慢 SQL、锁等待堆积或突发并发，常伴随 CPU/IO 升高。",
			ActionTemplate: "PROCESSLIST 看 State（Sending data/Locked/Waiting for…）；结合慢日志与 performance_schema；限流大查询；查行锁等待。",
			AlertNames: []string{"MySQLThreadsRunningHigh"}, Engine: "mysql",
			Tags: []string{"threads_running", "concurrency"},
		},
		{
			Fingerprint: "replication_lag|warning|above|mysql", MetricKey: "replication_lag",
			Severity: "warning", Direction: "above", Title: "MySQL 主从延迟偏高",
			RootCauseTemplate: "复制延迟约 {value}s：常见于主库大批量写/大事务、从库 IO 或 SQL 线程卡住、并行复制不足、从库被查询拖慢。",
			ActionTemplate: "SHOW REPLICA STATUS（或 SLAVE STATUS）看 IO/SQL Running、Last_Error、Seconds_Behind_Source；拆大事务；开并行复制；报表走只读并避开从库过载；核对网络与磁盘。",
			AlertNames: []string{"MySQLReplicationLag"}, Engine: "mysql",
			Tags: []string{"replication", "lag", "主从延迟"},
		},
		{
			Fingerprint: "replication_lag|critical|above|mysql", MetricKey: "replication_lag",
			Severity: "critical", Direction: "above", Title: "MySQL 复制中断或严重延迟",
			RootCauseTemplate: "复制延迟/中断 {value}：IO 或 SQL 线程停止、位点错误、表结构不一致均可导致只读业务读到过期数据。",
			ActionTemplate: "先看 Last_IO/SQL_Error；修权限/网络/表结构；必要时重搭从库；切换前确认延迟可接受；禁止在延迟未知时切主。",
			AlertNames: []string{"MySQLReplicationLag"}, Engine: "mysql",
			Tags: []string{"replication", "incident"},
		},
		{
			Fingerprint: "deadlocks_ps|warning|above|mysql", MetricKey: "deadlocks_ps",
			Severity: "warning", Direction: "above", Title: "MySQL 死锁增多",
			RootCauseTemplate: "死锁速率 {value}：交叉更新顺序、间隙锁与插入意向锁冲突、缺索引扩大锁范围是高频原因。",
			ActionTemplate: "SHOW ENGINE INNODB STATUS 看 LATEST DETECTED DEADLOCK；开 innodb_print_all_deadlocks 落 error log；统一加锁顺序；补索引；缩短事务；勿盲目调大 lock_wait_timeout。",
			AlertNames: []string{"MySQLDeadlocks"}, Engine: "mysql",
			Tags: []string{"deadlock", "innodb", "锁"},
		},
		{
			Fingerprint: "lock_waiters|warning|above|mysql", MetricKey: "lock_waiters",
			Severity: "warning", Direction: "above", Title: "MySQL 行锁等待堆积",
			RootCauseTemplate: "锁等待偏高（{value}）：长事务未提交、热点行更新、DDL 与 DML 互斥均可导致业务超时。",
			ActionTemplate: "查 performance_schema.data_locks / data_lock_waits；定位阻塞源事务并催提交或 kill；热点改为排队或拆分；DDL 用 Online DDL 并设锁等待超时。",
			AlertNames: []string{"MySQLLockWait", "MySQLThreadsRunningHigh"}, Engine: "mysql",
			Tags: []string{"lock", "innodb", "锁等待"},
		},
		{
			Fingerprint: "slow_query_cnt|warning|above|mysql", MetricKey: "slow_query_cnt",
			Severity: "warning", Direction: "above", Title: "MySQL 慢查询增多",
			RootCauseTemplate: "慢查询数 {value}：缺索引、统计过期、隐式转换、临时表落盘、执行计划突变均可导致。",
			ActionTemplate: "开 slow_query_log；pt-query-digest / 平台 Top SQL；EXPLAIN 验证；补复合索引；避免 SELECT *；检查 Created_tmp_disk_tables。",
			AlertNames: []string{"MySQLSlowQuery"}, Engine: "mysql",
			Tags: []string{"slow_query", "index", "explain"},
		},
		{
			Fingerprint: "cache_hit_ratio|warning|below|mysql", MetricKey: "cache_hit_ratio",
			Severity: "warning", Direction: "below", Title: "MySQL Buffer Pool 命中率偏低",
			RootCauseTemplate: "缓冲池命中 {value} 偏低：工作集大于 innodb_buffer_pool_size、全表扫增多或突然冷数据访问。",
			ActionTemplate: "查 Innodb_buffer_pool_reads；优化大扫 SQL；评估调大 buffer pool（留足 OS 缓存）；冷热分离与归档。",
			AlertNames: []string{"MySQLBufferPoolLow"}, Engine: "mysql",
			Tags: []string{"buffer_pool", "cache", "io"},
		},
		{
			Fingerprint: "disk_usage|warning|above|mysql", MetricKey: "disk_usage",
			Severity: "warning", Direction: "above", Title: "MySQL 数据/日志盘偏高",
			RootCauseTemplate: "磁盘使用率 {value} 偏高：数据增长、binlog/relaylog 堆积、undo/临时文件、未清理备份均可导致。",
			ActionTemplate: "区分 datadir 与 binlog 盘；查 binlog 保留策略与 expire_logs_days/binlog_expire_logs_seconds；清理过期备份；大表归档；规划扩容。",
			AlertNames: []string{"MySQLDisk", "HostDisk"}, Engine: "mysql",
			Tags: []string{"disk", "binlog", "storage"},
		},
		{
			Fingerprint: "binlog_lag|warning|above|mysql", MetricKey: "binlog_disk",
			Severity: "warning", Direction: "above", Title: "MySQL Binlog 堆积",
			RootCauseTemplate: "Binlog 体积/保留异常（{value}）：同步下游慢、保留时间过长、或异常大事务刷日志。",
			ActionTemplate: "SHOW BINARY LOGS；核对复制/Canal/CDC 消费位点；缩短过期时间；拆大事务；磁盘告急时谨慎 PURGE（确认无下游需要）。",
			AlertNames: []string{"MySQLDisk", "MySQLReplicationLag"}, Engine: "mysql",
			Tags: []string{"binlog", "cdc", "disk"},
		},
		{
			Fingerprint: "tmp_disk_tables|warning|above|mysql", MetricKey: "tmp_disk_tables",
			Severity: "warning", Direction: "above", Title: "MySQL 磁盘临时表过多",
			RootCauseTemplate: "Created_tmp_disk_tables 偏高（{value}）：排序/分组内存不足或大结果集迫使临时表落盘，拖慢查询并打满 IO。",
			ActionTemplate: "调 tmp_table_size/max_heap_table_size（适度）；优化 ORDER BY/GROUP BY 与索引；限制结果集；观察 IO 与慢日志。",
			AlertNames: []string{"MySQLSlowQuery"}, Engine: "mysql",
			Tags: []string{"tmp_table", "io", "slow_query"},
		},
		{
			Fingerprint: "long_trx|warning|above|mysql", MetricKey: "max_xact_duration",
			Severity: "warning", Direction: "above", Title: "MySQL 长事务",
			RootCauseTemplate: "长事务约 {value}s：阻碍 purge、导致 undo 膨胀与锁持有时间过长，拖累主从与热点更新。",
			ActionTemplate: "information_schema.innodb_trx 定位；催提交/回滚；禁止交互式长事务；批量拆分；设合理超时。",
			AlertNames: []string{"MySQLLongTransaction", "MySQLThreadsRunningHigh"}, Engine: "mysql",
			Tags: []string{"transaction", "undo", "长事务"},
		},
		{
			Fingerprint: "mysql_mha|info|manual", MetricKey: "mysql_ha",
			Severity: "info", Direction: "manual", Title: "MySQL MHA 部署要点",
			RootCauseTemplate: "MHA（Master High Availability）用于 MySQL 主从自动故障切换：依赖健康复制拓扑、管理节点与候选主可连接性；部署不当会导致切换失败或脑裂风险。",
			ActionTemplate: "1) 拓扑：1 主多从，GTID 或可靠位点，候选主与从库可互相 SSH/复制连通。" +
				"2) 复制健康：IO/SQL 线程正常，延迟可控，只读从库配置正确。" +
				"3) 安装：Manager + Node，配置 app.conf（user/password、repl、ssh、master/slaves）。" +
				"4) 权限：复制账号、MHA 管理账号、各节点免密 SSH；二次检查 firewall。" +
				"5) 演练：masterha_check_repl / masterha_check_status，维护窗口做一次切换演练；生产配合 VIP/ProxySQL 改写。",
			AlertNames: []string{"MySQLHA", "MySQLReplicationLag"}, Engine: "mysql",
			Tags: []string{"mha", "ha", "高可用", "部署", "切换"},
		},
		{
			Fingerprint: "mysql_proxysql|info|manual", MetricKey: "mysql_ha",
			Severity: "info", Direction: "manual", Title: "MySQL ProxySQL 读写分离要点",
			RootCauseTemplate: "ProxySQL 承担连接池与读写路由；规则错误、主机组权重或只读探测失败会导致写打到从库或连接打满。",
			ActionTemplate: "核对 mysql_servers/hostgroups、mysql_query_rules；监控连接与延迟；主库故障时与 MHA/Orchestrator 联动摘除；避免把管理会话也打满业务池。",
			AlertNames: []string{"MySQLHA", "MySQLConn"}, Engine: "mysql",
			Tags: []string{"proxysql", "读写分离", "ha"},
		},
		{
			Fingerprint: "oncall_triage|info|manual|mysql", MetricKey: "oncall_triage",
			Severity: "info", Direction: "manual", Title: "MySQL 值班排查套路",
			RootCauseTemplate: "高频故障：连接打满、慢 SQL、死锁/锁等待、主从延迟、磁盘/binlog。先定影响面再抓 processlist+慢日志+复制状态。",
			ActionTemplate: "范围→证据（PROCESSLIST/慢日志/INNODB STATUS/REPLICA STATUS）→止血（kill/限流）→根治（池、索引、事务边界）→复盘告警。",
			AlertNames: []string{"MySQLConn", "MySQLSlowQuery", "MySQLThreadsRunningHigh"}, Engine: "mysql",
			Tags: []string{"oncall", "triage"},
		},
	}
}
