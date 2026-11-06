# KeePlus / auto-dba v1.12.87

相对 [v1.12.86](changelog-v1.12.86.md)。数据迁移补齐 OSS 中转、审批开关和定时窗口；同步列表采集时补齐 Connect 上的槽位信息；权限申请列出库里已有账号。无 Agent 协议变更。

## 数据迁移

- 审批开启时，提交成功后关闭「新建迁移任务」，打开工单列表。审批关闭时不建工单：立即任务直接开始，定时任务进入待触发，并打开迁移列表。
- PostgreSQL 且传输方式为 OSS 时，按租户和源环境的 OSS 配置上传 CSV 再导入。其它库类型选择 OSS 会在预检查失败。直连复制和多表并行保持原样。
- 定时任务按触发窗口判断，默认 10 分钟。窗口内才启动，错过的状态为「错过触发」。同一租户里运行中或预检查中的任务数达到上限时，不再启动新的任务。
- 预检查失败的任务可以重新编辑对象并再次提交。详情里通过或拒绝时，同步关联工单的状态。
- 策略步骤可填写速度限制。校验可额外勾选结构对比，默认仍只核行数。

## 同步列表

- 采集结束后，按本租户的 Debezium 服务读取 PostgreSQL connector 的 `slot.name` 和 `topic.prefix`，补齐槽位上的 Kafka 地址、任务名和 topic。
- 已经对上本地 CDC 任务的行不覆盖。某一处 Connect 查询失败时，保留以前补上的信息。不会自动创建 CDC 任务。
- 使用前须重启后端，并在 Debezium 服务里登记对应地址，然后在同步列表点击「采集」。

启动时若缺列，会执行：

```sql
ALTER TABLE tbl_sync_slots ADD COLUMN connect_base TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_sync_slots ADD COLUMN connector_name TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_sync_slots ADD COLUMN task_name TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_sync_slots ADD COLUMN topic TEXT NOT NULL DEFAULT '';
```

## 权限申请

- 选好数据源后，用该实例的管理账号连到所选域名，列出库上已有账号。PostgreSQL 对齐 `\du`，不列出 `pg_` 开头的系统角色。
- 列表里的账号可以直接提交。手输且不在列表中的新账号仍须以 `_user` 结尾。读取失败时提示原因，不再退回只显示管理账号。

## 打包与发版

```bash
STAMP=v1.12.87 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.87 -m "v1.12.87"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
