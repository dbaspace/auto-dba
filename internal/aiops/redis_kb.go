package aiops

// RedisKnowledgeCases Redis/Tair 兼容运维知识库。
// 主题提炼自公开手册与社区案例（内存打满、大 Key/热 Key、慢查询、主从切换、复制积压），文案原创。
func RedisKnowledgeCases() []KnowledgeCase {
	return []KnowledgeCase{
		{
			Fingerprint: "mem_usage|warning|above|redis", MetricKey: "mem_usage",
			Severity: "warning", Direction: "above", Title: "Redis 内存使用率偏高",
			RootCauseTemplate: "内存使用率 {value} 偏高：无 TTL 堆积、大 Key、客户端输出缓冲、碎片率过高或写入突增均可导致；满后触发逐出或 OOM。",
			ActionTemplate: "INFO memory / MEMORY STATS / MEMORY DOCTOR；redis-cli --bigkeys 或缓存分析；补 TTL；调 maxmemory-policy；拆分大 Key；必要时扩容或改集群分片。",
			AlertNames: []string{"RedisMemHigh"}, Engine: "redis",
			Tags: []string{"memory", "maxmemory", "大key", "ttl"},
		},
		{
			Fingerprint: "mem_usage|critical|above|redis", MetricKey: "mem_usage",
			Severity: "critical", Direction: "above", Title: "Redis 内存危急/打满",
			RootCauseTemplate: "内存 {value} 危急：可能频繁 evict、延迟升高、写失败；紧急时防主机 OOM。",
			ActionTemplate: "临时确认 maxmemory 与 policy；UNLINK 删除确认可丢的大 Key；限流写入；扩容；禁止生产 KEYS*/FLUSHALL。",
			AlertNames: []string{"RedisMemHigh"}, Engine: "redis",
			Tags: []string{"memory", "incident", "oom"},
		},
		{
			Fingerprint: "bigkey|warning|above|redis", MetricKey: "bigkey_count",
			Severity: "warning", Direction: "above", Title: "Redis 存在大 Key",
			RootCauseTemplate: "大 Key 指示 {value}：HGETALL/LRANGE/DEL 等 O(N) 操作会阻塞单线程，引发慢查询甚至哨兵误判主从切换。",
			ActionTemplate: "MEMORY USAGE / HLEN/LLEN/ZCARD 确认；读写改 HSCAN/LRANGE 分页；删除用 UNLINK 或分批；Hash/List 拆分；Stream 注意 pending 堆积勿盲目 DEL。",
			AlertNames: []string{"RedisBigKey", "RedisSlowCommand"}, Engine: "redis",
			Tags: []string{"bigkey", "unlink", "阻塞"},
		},
		{
			Fingerprint: "hotkey|warning|above|redis", MetricKey: "hotkey_qps",
			Severity: "warning", Direction: "above", Title: "Redis 热 Key",
			RootCauseTemplate: "热 Key QPS 偏高（{value}）：单分片 CPU 打满、流控与超时；集群表现为个别节点倾斜。",
			ActionTemplate: "本地缓存/读写分离；Key 打散（加后缀分片）；限流；热点读可多副本；观察分片 CPU 与带宽。",
			AlertNames: []string{"RedisHotKey", "RedisCPUHigh"}, Engine: "redis",
			Tags: []string{"hotkey", "cpu", "倾斜"},
		},
		{
			Fingerprint: "slowlog|warning|above|redis", MetricKey: "slow_query_cnt",
			Severity: "warning", Direction: "above", Title: "Redis 慢查询增多",
			RootCauseTemplate: "慢日志条数/耗时偏高（{value}）：大 Key 操作、KEYS、复杂 Lua、网络阻塞命令均可导致后续请求排队。",
			ActionTemplate: "SLOWLOG GET；禁用 KEYS/FLUSHALL；大集合分批；优化 Lua；核对 client-output-buffer；关联是否触发 failover。",
			AlertNames: []string{"RedisSlowCommand"}, Engine: "redis",
			Tags: []string{"slowlog", "lua", "bigkey"},
		},
		{
			Fingerprint: "replication_lag|warning|above|redis", MetricKey: "replication_lag",
			Severity: "warning", Direction: "above", Title: "Redis 主从复制异常",
			RootCauseTemplate: "复制延迟/中断（{value}）：网络抖动、repl-backlog 被覆盖被迫全量、输出缓冲打满、从库消费慢。",
			ActionTemplate: "INFO replication；调大 repl-backlog-size；查 client-output-buffer-limit slave；避免高峰全量同步；观察主库 fork/COW 内存抖动。",
			AlertNames: []string{"RedisReplicationLag"}, Engine: "redis",
			Tags: []string{"replication", "psync", "backlog"},
		},
		{
			Fingerprint: "failover|warning|above|redis", MetricKey: "failover_events",
			Severity: "warning", Direction: "above", Title: "Redis 主从切换频繁",
			RootCauseTemplate: "切换事件 {value}：慢命令阻塞超过 down-after-milliseconds、网络分区、或资源打满导致哨兵误判。",
			ActionTemplate: "对齐哨兵日志与 SLOWLOG 时间线；排查 DEL 大 Key/Stream pending；调合理超时但优先消阻塞；防脑裂设 min-replicas-to-write。",
			AlertNames: []string{"RedisFailover"}, Engine: "redis",
			Tags: []string{"sentinel", "failover", "脑裂"},
		},
		{
			Fingerprint: "cpu_usage|warning|above|redis", MetricKey: "cpu_usage",
			Severity: "warning", Direction: "above", Title: "Redis CPU 偏高",
			RootCauseTemplate: "CPU {value} 偏高：热 Key、高 QPS 复杂命令、Lua、或持久化/复制压力。",
			ActionTemplate: "INFO commandstats；拆分热 Key；减少 O(N)；读写分离；集群扩分片。",
			AlertNames: []string{"RedisCPUHigh", "HostCPUHigh"}, Engine: "redis",
			Tags: []string{"cpu", "qps"},
		},
		{
			Fingerprint: "conn_usage_ratio|warning|above|redis", MetricKey: "conn_usage_ratio",
			Severity: "warning", Direction: "above", Title: "Redis 连接数偏高",
			RootCauseTemplate: "连接使用率 {value} 偏高：客户端未池化、短连接风暴、或闲置连接堆积。",
			ActionTemplate: "INFO clients；核对应用池与超时；CLIENT LIST 找异常来源；调 maxclients 需评估内存。",
			AlertNames: []string{"RedisConnHigh"}, Engine: "redis",
			Tags: []string{"connection", "clients"},
		},
		{
			Fingerprint: "persistence|warning|above|redis", MetricKey: "rdb_cow_mem",
			Severity: "warning", Direction: "above", Title: "Redis 持久化/Fork 压力",
			RootCauseTemplate: "RDB/AOF 相关压力（{value}）：BGSAVE fork 引起内存翻倍风险与延迟毛刺，高峰全量同步更危险。",
			ActionTemplate: "错峰持久化；评估 AOF 策略；监控 latest_fork_usec；内存紧张时避免同时全量同步与重写。",
			AlertNames: []string{"RedisPersistence", "RedisMemHigh"}, Engine: "redis",
			Tags: []string{"rdb", "aof", "fork"},
		},
		{
			Fingerprint: "oncall_triage|info|manual|redis", MetricKey: "oncall_triage",
			Severity: "info", Direction: "manual", Title: "Redis 值班排查套路",
			RootCauseTemplate: "优先对齐：内存、CPU、慢日志、复制状态、哨兵切换时间线；大 Key DEL 是切换惨案高频根因。",
			ActionTemplate: "INFO+SLOWLOG+复制→是否阻塞/大Key→止血用 UNLINK/限流→根治拆 Key/TTL/池化→复盘。",
			AlertNames: []string{"RedisMemHigh", "RedisSlowCommand", "RedisFailover"}, Engine: "redis",
			Tags: []string{"oncall", "triage"},
		},
	}
}
