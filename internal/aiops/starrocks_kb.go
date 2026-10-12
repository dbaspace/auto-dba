package aiops

// StarRocksKnowledgeCases StarRocks 运维知识库。
// 主题提炼自官方 FAQ / 社区运维共识（BE 宕机、副本、导入超时、查询内存、FE 连接），文案原创。
func StarRocksKnowledgeCases() []KnowledgeCase {
	return []KnowledgeCase{
		{
			Fingerprint: "be_down|warning|above|sr", MetricKey: "be_alive_ratio",
			Severity: "warning", Direction: "above", Title: "StarRocks BE 不可用/宕机",
			RootCauseTemplate: "BE 存活异常（指示 {value}）：心跳连续失败后被标非存活，随后可能触发 Tablet 克隆；长时间宕机会增大恢复与再平衡成本。",
			ActionTemplate: "SHOW BACKENDS；查 be.WARNING/be.INFO；机器维护先 DECOMMISSION；修复后重新加回；关注 max_backend_down_time_second 与不健康 Tablet。",
			AlertNames: []string{"SRBEDown"}, Engine: "starrocks",
			Tags: []string{"be", "heartbeat", "clone", "宕机"},
		},
		{
			Fingerprint: "tablet_unhealthy|warning|above|sr", MetricKey: "unhealthy_tablet",
			Severity: "warning", Direction: "above", Title: "StarRocks 不健康副本偏多",
			RootCauseTemplate: "不健康 Tablet {value}：版本落后、BE 丢失或迁移失败；导入在健康副本满足 write_quorum 时仍可成功，但查询/容灾风险上升。",
			ActionTemplate: "ADMIN SHOW REPLICA STATUS；看 tablet scheduler；区分增量/全量克隆；检查 storage_medium 与盘符匹配（HDD/SSD 配置错误会导致 migrate failed）。",
			AlertNames: []string{"SRTabletUnhealthy"}, Engine: "starrocks",
			Tags: []string{"tablet", "replica", "clone"},
		},
		{
			Fingerprint: "load_fail|warning|above|sr", MetricKey: "load_error_rate",
			Severity: "warning", Direction: "above", Title: "StarRocks 导入失败增多",
			RootCauseTemplate: "导入失败率 {value}：RPC 超时、query_timeout、Broker/Routine/INSERT 配置不当、空分区或源数据问题常见。",
			ActionTemplate: "SHOW LOAD / 按 label 查 fe.log 再对 be.INFO；调 streaming_load_rpc_max_alive_time_sec、tablet_writer_rpc_timeout_sec、write_buffer_size；异步导入勿以提交成功当完成。",
			AlertNames: []string{"SRLoadFail"}, Engine: "starrocks",
			Tags: []string{"load", "broker", "rpc", "导入"},
		},
		{
			Fingerprint: "load_timeout|warning|above|sr", MetricKey: "load_timeout",
			Severity: "warning", Direction: "above", Title: "StarRocks 导入/INSERT 超时",
			RootCauseTemplate: "导入超时（{value}）：大批量 INSERT INTO SELECT、RPC 存活时间不足或 query_timeout 过小。",
			ActionTemplate: "SET query_timeout；调 BE RPC 超时项并重启生效；拆分批次；检查「failed to send batch / TabletWriter add batch with unknown id」。",
			AlertNames: []string{"SRLoadFail", "SRQueryTimeout"}, Engine: "starrocks",
			Tags: []string{"timeout", "insert", "rpc"},
		},
		{
			Fingerprint: "query_timeout|warning|above|sr", MetricKey: "query_timeout_cnt",
			Severity: "warning", Direction: "above", Title: "StarRocks 查询超时增多",
			RootCauseTemplate: "查询超时 {value}：SQL 复杂、物化/统计不足、单查询内存打满、或 FE 规划过慢（Full GC）。",
			ActionTemplate: "先优化 SQL/分区裁剪/物化视图；调 exec_mem_limit（单查询）而非一味加 timeout；查 fe.gc；高并发可看 brpc_connection_type=pooled。",
			AlertNames: []string{"SRQueryTimeout"}, Engine: "starrocks",
			Tags: []string{"query", "timeout", "mem"},
		},
		{
			Fingerprint: "be_mem|warning|above|sr", MetricKey: "be_mem_usage",
			Severity: "warning", Direction: "above", Title: "StarRocks BE 内存偏高",
			RootCauseTemplate: "BE 内存 {value} 偏高：查询/导入内存、元数据与缓存；注意空闲时也可能因统计收集占用一定内存。",
			ActionTemplate: "BE 内存看板按查询/导入拆分；调低异常大查询；tc_use_memory_min 等参数按官方建议；避免无限制并发大查询。",
			AlertNames: []string{"SRBEMemHigh"}, Engine: "starrocks",
			Tags: []string{"memory", "be"},
		},
		{
			Fingerprint: "fe_conn|warning|above|sr", MetricKey: "conn_usage_ratio",
			Severity: "warning", Direction: "above", Title: "StarRocks FE 连接数偏高",
			RootCauseTemplate: "FE 连接偏高（{value}）：Reach limit of connections；空闲连接堆积或负载均衡未均衡多 FE。",
			ActionTemplate: "多 FE 负载均衡；调 wait_timeout 回收空闲；核对前端池大小；Follower FE 建议 ≥3 做高可用。",
			AlertNames: []string{"SRFEConnHigh"}, Engine: "starrocks",
			Tags: []string{"fe", "connection", "ha"},
		},
		{
			Fingerprint: "fe_gc|warning|above|sr", MetricKey: "fe_gc_pause",
			Severity: "warning", Direction: "above", Title: "StarRocks FE 规划慢/GC",
			RootCauseTemplate: "规划耗时或 GC 指示 {value}：复杂 SQL 逻辑优化超时、JVM 堆不足导致 Full GC。",
			ActionTemplate: "看 fe.gc.log；适当加大 FE JVM；调 new_planner_optimize_timeout；简化视图/嵌套；客户端连多 FE。",
			AlertNames: []string{"SRFEGC", "SRQueryTimeout"}, Engine: "starrocks",
			Tags: []string{"fe", "gc", "planner"},
		},
		{
			Fingerprint: "compaction|warning|above|sr", MetricKey: "compaction_score",
			Severity: "warning", Direction: "above", Title: "StarRocks Compaction 堆积",
			RootCauseTemplate: "Compaction 分数/堆积 {value}：高频小批量导入导致版本过多，查询与导入变慢，严重时影响建表/TRUNCATE 拿锁。",
			ActionTemplate: "观察 compaction 调度；合并导入批次；核对磁盘与 BE 负载；避免在堆积高峰做大量 schema/TRUNCATE。",
			AlertNames: []string{"SRCompactionLag"}, Engine: "starrocks",
			Tags: []string{"compaction", "version"},
		},
		{
			Fingerprint: "oncall_triage|info|manual|sr", MetricKey: "oncall_triage",
			Severity: "info", Direction: "manual", Title: "StarRocks 值班排查套路",
			RootCauseTemplate: "先分查询/导入/节点三类：SHOW PROC/BACKENDS、SHOW LOAD、不健康 Tablet；再对 fe/be 日志按 label 串联。",
			ActionTemplate: "影响面→BE/FE/副本健康→导入或查询证据→调超时前先优化→维护走下线流程→复盘。",
			AlertNames: []string{"SRBEDown", "SRLoadFail", "SRQueryTimeout"}, Engine: "starrocks",
			Tags: []string{"oncall", "triage"},
		},
	}
}
