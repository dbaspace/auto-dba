package aiops

import "strings"

// KnowledgeCase 确定性知识库条目（种子案例 / 文档同源）。
type KnowledgeCase struct {
	Fingerprint       string   `json:"fingerprint"`
	MetricKey         string   `json:"metric_key"`
	Severity          string   `json:"severity"`
	Direction         string   `json:"direction"`
	Title             string   `json:"title"`
	RootCauseTemplate string   `json:"root_cause_template"`
	ActionTemplate    string   `json:"action_template"`
	AlertNames        []string `json:"alert_names,omitempty"`
	Engine            string   `json:"engine"` // postgres | mysql | host | general
	Tags              []string `json:"tags,omitempty"`
}

// PostgresKnowledgeCases PostgreSQL DBA 知识库（Phase0 案例库增强）。
// 文案面向告警 Context Bundle / 洞察 Narrative，占位符 {value} 与 sqlinsight.renderTemplate 一致。
func PostgresKnowledgeCases() []KnowledgeCase {
	return []KnowledgeCase{
		{
			Fingerprint: "conn_usage_ratio|warning|above", MetricKey: "conn_usage_ratio",
			Severity: "warning", Direction: "above", Title: "PG 连接使用率偏高",
			RootCauseTemplate: "连接使用率 {value} 偏高：常见于连接池过大、空闲会话堆积、应用泄漏，或突发未复用连接；打满后普通用户 FATAL remaining connection slots。",
			ActionTemplate: "1) 确认是否真打满：对照 max_connections 与当前连接数（pg_stat_activity / 连接池监控），看 remaining/waiting 与告警阈值。" +
				"2) 看谁占连接：按 state、application_name、usename、client_addr 分组，找出 idle/active/异常来源。" +
				"3) 区分空闲与阻塞：重点查 idle in transaction、长事务与锁等待——空闲事务往往比单纯 active 更危险。" +
				"4) 核对连接池与应用：PgBouncer/应用池大小、泄漏、重试风暴、健康检查；勿盲目把 max_connections 加到上千；留足 superuser_reserved_connections。",
			AlertNames: []string{"PGConnHigh"}, Engine: "postgres",
			Tags: []string{"connection", "pg_stat_activity", "打满", "连接打满", "排查"},
		},
		{
			Fingerprint: "conn_usage_ratio|critical|above", MetricKey: "conn_usage_ratio",
			Severity: "critical", Direction: "above", Title: "PG 连接接近打满",
			RootCauseTemplate: "连接使用率 {value} 接近 max_connections：新连接将被拒绝，业务可能大面积失败。",
			ActionTemplate: "优先终止空闲/异常会话（尤其 idle in transaction 过久）；用超级用户登录抢救；临时提高 max_connections 需评估内存；尽快修复连接池与泄漏；检查连接风暴与健康检查打满。",
			AlertNames: []string{"PGConnHigh"}, Engine: "postgres",
			Tags: []string{"connection", "incident", "打满", "连接打满"},
		},
		{
			Fingerprint: "backends|warning|spike", MetricKey: "backends",
			Severity: "warning", Direction: "spike", Title: "PG 后端连接数环比飙升",
			RootCauseTemplate: "backends 相对基线飙升：多为流量突增、连接池失效直连、或批量作业并发过高。",
			ActionTemplate: "对比 TPS/活跃会话；确认是否连接池旁路；检查定时任务与发布窗口；必要时限流或扩容只读副本分流读。",
			AlertNames: []string{"PGConnHigh"}, Engine: "postgres",
			Tags: []string{"backends", "baseline"},
		},
		{
			Fingerprint: "cache_hit_ratio|warning|below", MetricKey: "cache_hit_ratio",
			Severity: "warning", Direction: "below", Title: "PG 缓存命中率偏低",
			RootCauseTemplate: "缓存命中率 {value} 偏低：可能存在全表扫描、shared_buffers 不足、工作集突然变大或统计信息过期导致差计划。",
			ActionTemplate: "结合 AWR/Top SQL 查 seq scan；检查缺失索引与膨胀表；评估 shared_buffers / effective_cache_size；对关键表 ANALYZE。",
			AlertNames: []string{"PGCacheHitLow"}, Engine: "postgres",
			Tags: []string{"cache", "shared_buffers", "index"},
		},
		{
			Fingerprint: "cache_hit_ratio|critical|below", MetricKey: "cache_hit_ratio",
			Severity: "critical", Direction: "below", Title: "PG 缓存命中率严重偏低",
			RootCauseTemplate: "缓存命中率 {value} 严重偏低，IO 压力通常同步升高，延迟与 CPU 可能一并恶化。",
			ActionTemplate: "立即定位热点全表扫描 SQL 并加索引或改写；检查是否误做大范围导出/VACUUM FULL；评估内存与磁盘 IO 瓶颈。",
			AlertNames: []string{"PGCacheHitLow"}, Engine: "postgres",
			Tags: []string{"cache", "incident"},
		},
		{
			Fingerprint: "replication_lag|warning|above", MetricKey: "replication_lag",
			Severity: "warning", Direction: "above", Title: "PG 复制延迟偏高",
			RootCauseTemplate: "复制延迟 {value}s：常见于网络抖动、备库 IO/查询压力、复制槽堆积、或主库长事务阻碍清理。",
			ActionTemplate: "查 pg_stat_replication / 复制槽；确认备库是否被重查询拖住；排查主库长事务与大事务；必要时临时只读限流。",
			AlertNames: []string{"PGReplicationLag"}, Engine: "postgres",
			Tags: []string{"replication", "ha"},
		},
		{
			Fingerprint: "replication_lag|critical|above", MetricKey: "replication_lag",
			Severity: "critical", Direction: "above", Title: "PG 复制延迟严重",
			RootCauseTemplate: "复制延迟 {value}s 严重：读写分离可能读到过旧数据，切换窗口风险上升。",
			ActionTemplate: "评估是否暂停重查询；检查复制槽膨胀与 WAL 堆积；确认网络与磁盘；准备切换预案并通知业务只读一致性风险。",
			AlertNames: []string{"PGReplicationLag"}, Engine: "postgres",
			Tags: []string{"replication", "incident"},
		},
		{
			Fingerprint: "max_xact_duration|warning|above", MetricKey: "max_xact_duration",
			Severity: "warning", Direction: "above", Title: "PG 存在长事务",
			RootCauseTemplate: "最长事务约 {value}s：长事务会阻碍 VACUUM、导致表膨胀，并可能拖累复制与热点更新。",
			ActionTemplate: "在会话管理定位 xid/时长/SQL；联系业务提交或回滚；检查 idle in transaction；评估 statement_timeout / idle_in_transaction_session_timeout。",
			AlertNames: []string{"PGLongTransaction"}, Engine: "postgres",
			Tags: []string{"xact", "vacuum"},
		},
		{
			Fingerprint: "max_xact_duration|critical|above", MetricKey: "max_xact_duration",
			Severity: "critical", Direction: "above", Title: "PG 存在超长事务",
			RootCauseTemplate: "最长事务约 {value}s：可能阻塞复制槽推进与 HOT 更新，存储与延迟风险显著。",
			ActionTemplate: "尽快终止或督促提交异常会话；同步检查 replication_lag 与表膨胀；复核批量作业事务边界。",
			AlertNames: []string{"PGLongTransaction"}, Engine: "postgres",
			Tags: []string{"xact", "incident"},
		},
		{
			Fingerprint: "deadlocks_ps|warning|above", MetricKey: "deadlocks_ps",
			Severity: "warning", Direction: "above", Title: "PG 死锁增多",
			RootCauseTemplate: "死锁速率 {value}：多为交叉更新顺序不一致、缺索引导致锁范围扩大、或热点行并发冲突。",
			ActionTemplate: "查日志 deadlock 明细与涉及表；统一事务内更新顺序；补索引缩小锁范围；降低热点行并发或改为排队。",
			AlertNames: []string{"PGDeadlocks"}, Engine: "postgres",
			Tags: []string{"lock", "deadlock"},
		},
		{
			Fingerprint: "slow_query_cnt|warning|above", MetricKey: "slow_query_cnt",
			Severity: "warning", Direction: "above", Title: "PG 慢查询增多",
			RootCauseTemplate: "慢查询数 {value}：计划退化、数据量增长、缺索引或资源争用（CPU/IO/锁）均可导致。",
			ActionTemplate: "用 AWR/Top SQL 定位语句；EXPLAIN (ANALYZE, BUFFERS)；检查统计信息与膨胀；必要时限流或加只读副本。",
			AlertNames: []string{"PGSlowQuery"}, Engine: "postgres",
			Tags: []string{"sql", "performance"},
		},
		{
			Fingerprint: "slow_query_cnt|warning|spike", MetricKey: "slow_query_cnt",
			Severity: "warning", Direction: "spike", Title: "PG 慢查询环比飙升",
			RootCauseTemplate: "慢查询相对基线飙升：常与发布、统计信息过期、参数变更或突发大查询相关。",
			ActionTemplate: "对齐变更窗口与部署；对比前后 Top SQL；对关键表 ANALYZE；回滚可疑参数或限流大查询。",
			AlertNames: []string{"PGSlowQuery"}, Engine: "postgres",
			Tags: []string{"sql", "baseline"},
		},
		{
			Fingerprint: "tps|warning|drop", MetricKey: "tps",
			Severity: "warning", Direction: "drop", Title: "PG TPS 环比下跌",
			RootCauseTemplate: "TPS 相对基线下跌：可能是业务流量下降、主库只读/故障、锁阻塞或连接耗尽导致吞吐下降。",
			ActionTemplate: "确认主库可用性与只读状态；查锁等待与长事务；核对连接是否打满；对照业务流量与发布变更。",
			AlertNames: []string{"PGTPSDrop"}, Engine: "postgres",
			Tags: []string{"tps", "availability"},
		},
		{
			Fingerprint: "disk_usage|warning|above", MetricKey: "disk_usage",
			Severity: "warning", Direction: "above", Title: "PG 存储使用率偏高",
			RootCauseTemplate: "存储使用率 {value} 偏高：表膨胀、复制槽/归档失败导致 WAL 堆积、临时文件或业务数据增长均可导致。",
			ActionTemplate: "区分数据盘与 pg_wal；查大表/死元组与 pg_replication_slots；确认 archive 是否失败；清理过期数据与临时文件；规划扩容。",
			AlertNames: []string{"PGStorage"}, Engine: "postgres",
			Tags: []string{"storage", "bloat", "wal"},
		},
		{
			Fingerprint: "disk_usage|critical|above", MetricKey: "disk_usage",
			Severity: "critical", Direction: "above", Title: "PG 存储使用率危急",
			RootCauseTemplate: "存储使用率 {value} 危急：写满后无法写 WAL，提交失败，云上常见锁成只读。",
			ActionTemplate: "立即扩容或清理可删数据/日志；停止非关键大批量写入；优先处理最滞后复制槽与失败归档；准备只读降级预案。",
			AlertNames: []string{"PGStorage"}, Engine: "postgres",
			Tags: []string{"storage", "incident", "wal"},
		},
		{
			Fingerprint: "cpu_usage|warning|above", MetricKey: "cpu_usage",
			Severity: "warning", Direction: "above", Title: "PG/主机 CPU 偏高",
			RootCauseTemplate: "CPU 使用率 {value} 偏高：常与差 SQL、并行查询过多、或连接风暴相关。",
			ActionTemplate: "结合 Top SQL 与 wait 事件；限制并行度；优化高 CPU 语句；确认是否非数据库进程争用。",
			AlertNames: []string{"HostCPUHigh"}, Engine: "postgres",
			Tags: []string{"cpu", "host"},
		},
		{
			Fingerprint: "active_session_count|warning|above", MetricKey: "active_session_count",
			Severity: "warning", Direction: "above", Title: "PG 活跃会话偏多",
			RootCauseTemplate: "活跃会话数 {value} 偏高：可能存在锁等待堆积、慢 SQL 占满 worker，或突发并发。",
			ActionTemplate: "会话管理按 wait_event/state 分组；优先处理阻塞链源头；核对慢 SQL 与连接池。",
			AlertNames: []string{"PGActiveSessions"}, Engine: "postgres",
			Tags: []string{"session", "concurrency"},
		},
		// —— 以下条目提炼自 universe_fe/PostgreSQL知识图谱（DBA Daily / 运维监控 / 生产风险 / WAL）——
		{
			Fingerprint: "wal_size|warning|above", MetricKey: "wal_retain_bytes",
			Severity: "warning", Direction: "above", Title: "PG WAL 体积偏大",
			RootCauseTemplate: "pg_wal / WAL 体积偏大（指示 {value}）：常见于复制槽滞后未回收、归档失败堵住段删除、max_wal_size 过大、逻辑解码/CDC 慢、长事务或大量写放大（批量 UPDATE/DELETE、REPLICA IDENTITY FULL）。",
			ActionTemplate: "1) 查 pg_replication_slots 最落后 restart_lsn 与 retain_bytes，废弃槽立刻 drop。" +
				"2) 查 archive_mode/archive_command 是否失败。" +
				"3) 对照 max_wal_size / checkpoint，避免无意义频繁 switch。" +
				"4) 区分「写得多」与「删不掉」：前者看业务 WAL 产量，后者优先槽与归档。" +
				"5) 设 max_slot_wal_keep_size 保主库存活。",
			AlertNames: []string{"PGStorage"}, Engine: "postgres",
			Tags: []string{"wal", "pg_wal", "wal大", "wal高", "wal涨"},
		},
		{
			Fingerprint: "wal_retain_bytes|warning|above", MetricKey: "wal_retain_bytes",
			Severity: "warning", Direction: "above", Title: "PG 复制槽滞留 WAL",
			RootCauseTemplate: "复制槽滞后导致主库 WAL 滞留约 {value}：WAL 能删到哪由最落后槽的 restart_lsn 决定；废弃/INACTIVE 槽与慢消费者同样会钉盘，逻辑槽还会钉 catalog_xmin。",
			ActionTemplate: "查 pg_replication_slots（slot_name/active/restart_lsn/retain_bytes）；对废弃槽 pg_drop_replication_slot；督促 CDC/备库消费；设 max_slot_wal_keep_size 保主库；多下游应共用一槽再进消息队列。",
			AlertNames: []string{"PGStorage", "PGReplicationLag"}, Engine: "postgres",
			Tags: []string{"wal", "slot", "replication", "wal大", "wal高"},
		},
		{
			Fingerprint: "wal_retain_bytes|critical|above", MetricKey: "wal_retain_bytes",
			Severity: "critical", Direction: "above", Title: "PG WAL/槽滞留危急",
			RootCauseTemplate: "WAL 滞留 {value} 危急：pg_wal 写满后提交失败，云上常见实例锁成只读。",
			ActionTemplate: "立即处理最滞后槽（消费或删除）；扩容或临时腾挪磁盘；核对 archive_command 是否失败堵住回收；备库长期断开勿占着槽。",
			AlertNames: []string{"PGStorage", "PGReplicationLag"}, Engine: "postgres",
			Tags: []string{"wal", "slot", "incident"},
		},
		{
			Fingerprint: "dead_tup_ratio|warning|above", MetricKey: "dead_tup_ratio",
			Severity: "warning", Direction: "above", Title: "PG 表膨胀/死元组偏高",
			RootCauseTemplate: "死元组比例约 {value}：磁盘涨得比业务快、同 SQL 变慢常见于 MVCC 膨胀；长事务与 idle in transaction 会挡住 Vacuum 回收。",
			ActionTemplate: "查 pg_stat_user_tables（n_dead_tup/last_autovacuum）；先杀/督促超长事务；保证 autovacuum；热点表降 scale_factor；勿高峰 VACUUM FULL，空间告急用 pg_repack 或分区 DROP。",
			AlertNames: []string{"PGStorage", "PGLongTransaction"}, Engine: "postgres",
			Tags: []string{"bloat", "vacuum", "mvcc"},
		},
		{
			Fingerprint: "autovacuum_lag|warning|above", MetricKey: "autovacuum_lag",
			Severity: "warning", Direction: "above", Title: "PG Autovacuum 跟不上",
			RootCauseTemplate: "Autovacuum 滞后（指示 {value}）：n_dead_tup 持续高、last_autovacuum 过旧，或 worker 打满/被长事务挡住；严重时出现 wraparound 预警。",
			ActionTemplate: "看 pg_stat_progress_vacuum 与 autovacuum_max_workers；大表单独加严阈值、低峰加快 cost_delay；先处理长事务再指望 freeze；禁止关掉 autovacuum。",
			AlertNames: []string{"PGStorage", "PGLongTransaction"}, Engine: "postgres",
			Tags: []string{"vacuum", "autovacuum"},
		},
		{
			Fingerprint: "xid_age|warning|above", MetricKey: "xid_age",
			Severity: "warning", Direction: "above", Title: "PG 事务 ID 年龄偏高",
			RootCauseTemplate: "库/表 XID 年龄约 {value}：接近 wraparound 时会拒绝写入以保护数据；prepared xact、复制槽与超长事务都会卡住冻结。",
			ActionTemplate: "查 pg_database.age(datfrozenxid) 与最老 relfrozenxid；分级监控（如 3亿/8亿/15亿）；清僵尸 prepared xact 与废弃槽；对最老表 VACUUM (FREEZE)。",
			AlertNames: []string{"PGLongTransaction"}, Engine: "postgres",
			Tags: []string{"xid", "wraparound", "vacuum"},
		},
		{
			Fingerprint: "xid_age|critical|above", MetricKey: "xid_age",
			Severity: "critical", Direction: "above", Title: "PG XID wraparound 危急",
			RootCauseTemplate: "XID 年龄 {value} 危急：临近或已触发 wraparound 保护，写入可能被拒绝。",
			ActionTemplate: "按 P0 处理：立即对最老库/表 freeze；清 pg_prepared_xacts 与废弃槽；确认 autovacuum 未关闭；事后把年龄告警做成监控而非事后查文档。",
			AlertNames: []string{"PGLongTransaction"}, Engine: "postgres",
			Tags: []string{"xid", "wraparound", "incident"},
		},
		{
			Fingerprint: "lock_waiters|warning|above", MetricKey: "lock_waiters",
			Severity: "warning", Direction: "above", Title: "PG 锁等待堆积",
			RootCauseTemplate: "锁等待会话约 {value}：交叉加锁、缺索引扩大锁范围、DDL/VACUUM FULL 持 AccessExclusiveLock，或热点行并发冲突均可导致。",
			ActionTemplate: "用阻塞链查询定位源头（pg_locks / pg_stat_activity）；短事务、统一加锁顺序；补索引缩小锁范围；DDL 设 lock_timeout；开 log_lock_waits。",
			AlertNames: []string{"PGActiveSessions", "PGDeadlocks"}, Engine: "postgres",
			Tags: []string{"lock", "concurrency"},
		},
		{
			Fingerprint: "idle_in_transaction|warning|above", MetricKey: "idle_in_transaction",
			Severity: "warning", Direction: "above", Title: "PG idle in transaction 偏多",
			RootCauseTemplate: "idle in transaction 约 {value}：事务已开但人/应用已离开，快照与锁仍在，Vacuum 与 xmin horizon 被拖住，比单纯长查询更危险。",
			ActionTemplate: "会话管理筛 state=idle in transaction；设 idle_in_transaction_session_timeout；禁止事务内等人机/慢 HTTP；报表走副本；对照槽与 prepared xact 四源 xmin。",
			AlertNames: []string{"PGLongTransaction", "PGConnHigh"}, Engine: "postgres",
			Tags: []string{"xact", "idle", "vacuum"},
		},
		{
			Fingerprint: "invalid_index|warning|above", MetricKey: "invalid_index",
			Severity: "warning", Direction: "above", Title: "PG 存在无效索引",
			RootCauseTemplate: "检测到无效索引（指示 {value}）：CREATE INDEX CONCURRENTLY 失败或中断会留下 INVALID 索引，优化器不用却仍占维护成本，易误判为「加了索引仍慢」。",
			ActionTemplate: "查 pg_index WHERE NOT indisvalid；DROP 失败残留后重建；高峰用 CONCURRENTLY；重建后 EXPLAIN 验证是否走 Index Scan。",
			AlertNames: []string{"PGSlowQuery"}, Engine: "postgres",
			Tags: []string{"index", "sql"},
		},
		{
			Fingerprint: "archive_lag|warning|above", MetricKey: "archive_lag",
			Severity: "warning", Direction: "above", Title: "PG WAL 归档落后",
			RootCauseTemplate: "归档滞后约 {value}：archive_command/archive_library 失败会堵住 WAL 段回收，pg_wal 持续膨胀直至磁盘告急。",
			ActionTemplate: "查 archive_mode/archive_command 与日志报错；修复目标存储/权限后确认可切换段；必要时临时扩容；与复制槽滞留区分排查。",
			AlertNames: []string{"PGStorage"}, Engine: "postgres",
			Tags: []string{"wal", "archive"},
		},
		{
			Fingerprint: "seq_scan_ratio|warning|above", MetricKey: "seq_scan_ratio",
			Severity: "warning", Direction: "above", Title: "PG 大表顺序扫描偏多",
			RootCauseTemplate: "顺序扫占比/次数偏高（{value}）：缺索引、统计过期、计划漂移（角色/search_path/work_mem 不同）或无效索引均可导致。",
			ActionTemplate: "黄金三问：I/O 多吗、CPU 高吗、等锁了吗；对 Top SQL EXPLAIN (ANALYZE,BUFFERS)；对齐同一角色与 GUC 比计划；补/修索引，避免过度索引。",
			AlertNames: []string{"PGSlowQuery"}, Engine: "postgres",
			Tags: []string{"sql", "index", "plan"},
		},
		{
			Fingerprint: "checkpoint_io|warning|above", MetricKey: "checkpoint_io",
			Severity: "warning", Direction: "above", Title: "PG 检查点写放大/IO 尖峰",
			RootCauseTemplate: "检查点相关 IO 偏高（{value}）：checkpoint 过频导致刷盘尖峰与写放大；过稀则崩溃恢复久、pg_wal 变肥。",
			ActionTemplate: "核对 checkpoint_timeout 与 max_wal_size；观察 checkpoints_timed/req；高峰避免人为频繁 SWITCH WAL；结合共享缓冲与脏页比例调优。",
			AlertNames: []string{"HostCPUHigh", "PGStorage"}, Engine: "postgres",
			Tags: []string{"checkpoint", "wal", "io"},
		},
		{
			Fingerprint: "backup_drill|info|manual", MetricKey: "backup_status",
			Severity: "info", Direction: "manual", Title: "PG 备份未演练等于没有",
			RootCauseTemplate: "备份是最后防线：仅有 pg_basebackup/逻辑备份而无恢复演练，故障时不可用；需含 WAL 才能 PITR。",
			ActionTemplate: "核对物理+WAL 归档链路；定期在隔离环境做真恢复；故障切换与恢复演练写入 runbook；监控备份成功与归档连续性。",
			AlertNames: []string{"PGStorage"}, Engine: "postgres",
			Tags: []string{"backup", "pitr", "ha"},
		},
		{
			Fingerprint: "oncall_triage|info|manual", MetricKey: "oncall_triage",
			Severity: "info", Direction: "manual", Title: "PG 值班五步诊断",
			RootCauseTemplate: "学徒 DBA Daily：表象（用户/告警）→ 范围（影响面）→ 指标（找异常）→ 根因（假设验证）→ 解决（修复、验证、复盘）。心法：备份最后防线、监控最早预警、自动化最稳队友、文档最好传承。",
			ActionTemplate: "收齐事实再动手；优先看连接/复制延迟/命中率/WAL/磁盘/死元组；记下→复盘→自动化→预防；危险操作先在克隆演练，生产 DDL 带 lock_timeout。",
			AlertNames: []string{"PGActiveSessions", "PGSlowQuery", "PGStorage"}, Engine: "postgres",
			Tags: []string{"oncall", "triage", "runbook"},
		},
	}
}

// GeneralKnowledgeCases 跨引擎通用案例（主机等）。
func GeneralKnowledgeCases() []KnowledgeCase {
	return []KnowledgeCase{
		{
			Fingerprint: "disk_usage|warning|above|host", MetricKey: "disk_usage",
			Severity: "warning", Direction: "above", Title: "主机磁盘使用率偏高",
			RootCauseTemplate: "主机根分区磁盘使用率 {value} 偏高。",
			ActionTemplate: "清理日志与临时文件；确认挂载点；规划扩容。",
			AlertNames: []string{"HostDisk"}, Engine: "host",
			Tags: []string{"disk"},
		},
		{
			Fingerprint: "cpu_usage|warning|above|host", MetricKey: "cpu_usage",
			Severity: "warning", Direction: "above", Title: "主机 CPU 偏高",
			RootCauseTemplate: "主机 CPU 使用率 {value} 偏高：可能是数据库进程或其它负载争用。",
			ActionTemplate: "top/pidstat 区分进程；对照库内慢 SQL/热 Key；排除非 DB 进程后再调库参数。",
			AlertNames: []string{"HostCPUHigh"}, Engine: "host",
			Tags: []string{"cpu", "host"},
		},
	}
}

// AllKnowledgeCases 全部知识库条目（PG / MySQL / Redis / StarRocks / 主机）。
func AllKnowledgeCases() []KnowledgeCase {
	out := append([]KnowledgeCase{}, PostgresKnowledgeCases()...)
	out = append(out, MySQLKnowledgeCases()...)
	out = append(out, RedisKnowledgeCases()...)
	out = append(out, StarRocksKnowledgeCases()...)
	out = append(out, GeneralKnowledgeCases()...)
	return out
}

// KnowledgeCasesByEngine 按引擎过滤（空=全部）。
func KnowledgeCasesByEngine(engine string) []KnowledgeCase {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" || engine == "all" {
		return AllKnowledgeCases()
	}
	var out []KnowledgeCase
	for _, c := range AllKnowledgeCases() {
		if strings.ToLower(c.Engine) == engine {
			out = append(out, c)
		}
	}
	return out
}
