# KeePlus / auto-dba v1.12.31

相对 [v1.12.30](changelog-v1.12.30.md)。无表结构变更。启动时补「实时动态」菜单、域名「删除」动作，以及全局参数 `dns_forward_fail_ratio`（缺省 `0.05`）。

## 告警实时动态

- 告警管理新增「实时动态」（`/ops/alerts/activity`）。把告警触发与恢复、工单、高可用、配置变更、DNS、AWR 按时间归成一条时间线。
- 接口：`GET /api/v1/alerts/activity`。

## CDC

- 进程启动时，把超过 5 分钟仍停在编排中的任务标为失败，可从失败步骤重试。
- 同一源库、同一组 Kafka 的编排串行；PostgreSQL 编排连接带锁等待与语句超时。创建复制槽被堵住时，结束空闲超过 60 秒的 `idle in transaction` 会话后再试。
- 复制槽已满时，错误带上当前占用和 `max_replication_slots`。
- 槽采集的滞后按 `restart_lsn` 到当前 WAL 计算；初始化任务优先于加表子任务。批量重启不再把 `*-add` 任务名当成 Connector。
- StarRocks 建表补 `is_physical_deleted`、`source_db_name`、`source_table_name`。Routine Load 已覆盖这些列则不重建，避免把消费位点重置。
- 初始化任务可「修复源端」：只改 Connector 的主机和端口，槽名、publication、表清单和账号不动。版本来源记为 `repair_source`。
- Debezium 配置页切走页签时保留未保存编辑。CDC 同步工单审批结束后回到同步任务的审批结果页。表清单较长时创建页和表映射用虚拟列表。

## 域名与已登记库

- 域名管理可单条或批量删除。删除后从关联实例的域名列表去掉这一条，不删除实例，避免一键同步把域名再建回来。权限 `um.action.domains.delete`；非平台审计只能删本租户域名。
- 已登记库支持批量删除，不删除实例。接口：`POST /api/v1/databases/batch-delete`。

## DNS

- 节点巡检增加上游转发失败率（`coredns_forward_responses_total` 中 SERVFAIL 或 REFUSED）。阈值是全局参数 `dns_forward_fail_ratio`，缺省 `0.05`。

## 实例总览

- 列表补充角色与异常标记。PostgreSQL 已用存储按各库文件合计，使用率仍取云监控最近值。

## 打包与发版

```bash
STAMP=v1.12.31 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.31
```

部署见 [deploy.md](deploy.md)。
