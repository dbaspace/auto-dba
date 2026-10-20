package aiops

// K8sKnowledgeCases Kubernetes / 数据库上 K8s 诊断案例（P4）。
func K8sKnowledgeCases() []KnowledgeCase {
	return []KnowledgeCase{
		{
			Fingerprint: "k8s_node_notready|critical|above", MetricKey: "k8s_node_ready",
			Severity: "critical", Direction: "above", Title: "K8s 节点 NotReady",
			RootCauseTemplate: "节点 Ready=False：常见于 kubelet 宕机、磁盘/内存压力、CNI 故障或云主机异常。",
			ActionTemplate: "1) kubectl describe node / 平台节点页看 Conditions。" +
				"2) 检查 kubelet、容器运行时日志与磁盘使用。" +
				"3) 必要时 cordon 后排障；影响分析看 CMDB 上该节点关联的数据库实例。",
			AlertNames: []string{"K8sNodeNotReady"}, Engine: "k8s",
			Tags: []string{"kubernetes", "node", "NotReady"},
		},
		{
			Fingerprint: "k8s_pod_crashloop|critical|above", MetricKey: "k8s_pod_restarts",
			Severity: "critical", Direction: "above", Title: "Pod CrashLoopBackOff",
			RootCauseTemplate: "容器反复重启：配置错误、依赖不可达、OOM、探针失败或镜像问题。",
			ActionTemplate: "1) 查看 Pod 日志（previous）与 events。" +
				"2) describe 看 Last State / 探针。" +
				"3) 若为数据库 Operator Pod，对照 CR status 与资源 requests/limits。",
			AlertNames: []string{"K8sPodCrashLoop"}, Engine: "k8s",
			Tags: []string{"kubernetes", "pod", "CrashLoopBackOff"},
		},
		{
			Fingerprint: "k8s_oom|critical|above", MetricKey: "k8s_oomkilled",
			Severity: "critical", Direction: "above", Title: "容器 OOMKilled",
			RootCauseTemplate: "容器因内存超限被杀：limits 过小、泄漏或突发查询。",
			ActionTemplate: "上调 memory limits / 优化 SQL；对 CNPG/Percona 等同步调整 CR resources；观察重启后是否复发。",
			AlertNames: []string{"K8sOOMKilled"}, Engine: "k8s",
			Tags: []string{"kubernetes", "OOM", "memory"},
		},
		{
			Fingerprint: "k8s_pvc_high|warning|above", MetricKey: "k8s_pvc_usage",
			Severity: "warning", Direction: "above", Title: "PVC 使用率过高",
			RootCauseTemplate: "卷使用率过高：数据增长、WAL/binlog 堆积或未清理备份。",
			ActionTemplate: "扩容 PVC（若 StorageClass 支持）；清理无用数据；检查数据库归档与备份策略。",
			AlertNames: []string{"K8sPVCHigh"}, Engine: "k8s",
			Tags: []string{"kubernetes", "pvc", "storage"},
		},
	}
}
