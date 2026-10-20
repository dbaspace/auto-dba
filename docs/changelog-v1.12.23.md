# KeePlus / auto-dba v1.12.23

相对 [v1.12.22](changelog-v1.12.22.md)。已有库启动时补 K8s 相关表与「容器管理」菜单（`schema_version` / `seed_version` 递增），无需单独执行 SQL。

## 容器管理（K8s）

- 多集群纳管（kubeconfig / Token）、探测、命名空间划拨；概览、节点、工作负载、网络、存储只读；Pod 日志、资源浏览器、YAML dry-run/apply、扩缩/重启、cordon/drain。
- Operator 检测（CNPG / Percona / Redis / StarRocks / KubeBlocks）；数据库服务模板化创建与运维，创建后可登记实例（`source=k8s`）。
- `cmd/k8s-agent` + Helm Chart 反向隧道骨架；URN 权限、CMDB `k8s_*` 类型与拓扑边、K8s 告警种子规则。方案见 [k8s.md](k8s.md)。

## CDC

- 列表「异常」与「重新初始化」对齐老平台：Kafka MISSING/PARTIAL 不降级；手动 DDL（`auto_create_table=false`）豁免 SR/RL；仅 Connector FAILED / NOT_FOUND / task 全挂时展示重初始化。
- 分表 Kafka topic 判定增强（`IsShardMapping`）；pipeline topic 收集优先 mapping、空则回落库 `kafka_topics`。详情 / 列表展示与样式继续打磨。

## 其它

- 定时任务页交互小改；RBAC 文档补充 K8s 相关 URN。

## 打包与发版

```bash
STAMP=v1.12.23 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.23
```

部署见 [deploy.md](deploy.md)。
