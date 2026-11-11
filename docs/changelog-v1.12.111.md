# KeePlus / auto-dba v1.12.111

相对 [v1.12.110](changelog-v1.12.110.md)。云 PostgreSQL 变配按实际改动的规格或磁盘执行，规格带上最大存储。慢日志可经独立 Kafka 入库。查询窗口不再把 `LIMIT n OFFSET m` 拼成两条。无 Agent 协议变更。元库为慢日志游标和去重补列。

## 云 PostgreSQL

- 规格列表显示核数、内存和最大存储。内存按 GB 与云控制台对齐。磁盘超过该规格上限时提交前拒绝。
- PostgreSQL 和 MySQL 的扩盘并进变配。目标规格不填或与原来相同则不调规格；磁盘不改则不扩容。只把有变化的核数、内存或磁盘发给云 API。
- 变配和扩容可选切换时间：升级完成时、指定时间或维护窗口。
- 新建实例拉取可用区、版本、VPC、子网、安全组和项目。
- 执行变更时没有单独的操作密钥，就用同步用的只读密钥。工单申请信息展示执行结果；云 API 拒绝权限时写明是密钥没有该操作权限。
- 云资源工单只能由平台管理员、dba 租户用户或全局审核白名单审批。

## 慢日志

- `slowlog_bus.brokers` 留空时仍直接写入元库。配上地址后，采集先发到主题 `cloud-pg-slowlog`，再由平台消费入库。可按云实例 ID 灰度。这套 Kafka 与 CDC 的 `kafka_brokers` 分开，账号密码只放 Nacos。
- 页面增加趋势、分布和按 SQL 汇总。游标显示已发到 Kafka、已入库的时间。同一条慢日志按去重键只保留一次。
- 同步游标增加缺口区间和发送、入库时间，供补采使用。

## 查询与审核

- PostgreSQL 的 DDL 检测放行 `BEGIN` / `COMMIT` / `ROLLBACK`，以及只设置 `lock_timeout`、`statement_timeout` 的 `SET` / `SET LOCAL`。`SET LOCAL` 必须写在 `BEGIN` 和 `COMMIT` 之间。执行仍逐条跑，不再套一层事务。MySQL 不放行。
- 查询窗口识别语句末尾的 `LIMIT n OFFSET m` 和 `LIMIT offset, count`，只保留这一条并按最大行数钳制，不再在后面再拼一条 `limit`。

## 同步列表与 StarRocks 集群

- 同步列表在确认采集进度之前禁用批量重启。采集进行中只提示一次，不再叠两条红色报错。采集与重启的互斥不变。
- 节点已停止时，不再因为 `SHOW PROC` 仍显示 Alive 就把状态改回运行中；只有 systemd 仍是 active 才写回 ready。当前 Leader 与首 FE 标记对齐。

## 打包与发版

```bash
STAMP=v1.12.111 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.111 -m "v1.12.111"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。云变配、慢日志和查询改写要使用本版平台进程，请重启平台后再用。已有元库若跳过启动迁移，先执行文末 SQL。

```sql
ALTER TABLE tbl_cloud_sync_cursors ADD COLUMN IF NOT EXISTS gap_from TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_cloud_sync_cursors ADD COLUMN IF NOT EXISTS gap_to TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_cloud_sync_cursors ADD COLUMN IF NOT EXISTS produced_at TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_cloud_sync_cursors ADD COLUMN IF NOT EXISTS ingested_at TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_cloud_slow_logs ADD COLUMN IF NOT EXISTS dedupe_key TEXT NOT NULL DEFAULT '';
DROP INDEX IF EXISTS uq_cloud_slow;
CREATE UNIQUE INDEX IF NOT EXISTS uq_cloud_slow_dedupe ON tbl_cloud_slow_logs(vendor, engine, cloud_instance_id, dedupe_key) WHERE dedupe_key <> '';
```
