# KeePlus / auto-dba v1.12.78

相对 [v1.12.77](changelog-v1.12.77.md)。无表结构变更。

## CDC 加表与巡检

- 审核人只接受平台管理员、`issue_global_approver_whitelist`、`dba` 租户活跃成员。登记库/实例审批人不再作为 CDC 审核人。
- 工单原因只保留人手写的说明；未写则为空，不再把 CDC 配置 JSON 放进原因。
- 待审核详情审批改绑到仍处于待审批的 `cdc_sync` 工单，避免点到父任务已通过的工单。
- 选表字段改为按批查询；远程机房经 sql-agent `/v1/sql/meta` 的 `column_tables` 就近拉列。须用本版包重下发 sql-agent，旧 Agent 仍走直连兜底。
- 加表子任务继承父任务生效源端域名（`source_host` 或 Debezium `database.hostname`），连接与详情不再落到实例默认地址。
- 映射选「初始化」，或加表任务 `snapshot_mode` 为 `initial` 时，向 `debezium_signal` 写入本次新增表的 `execute-snapshot`。
- 表加入 publication 后，对本次加入的源表执行 `ALTER TABLE … REPLICA IDENTITY FULL`。
- 结构巡检按任务记下的 StarRocks 主机匹配已登记实例；对不上时区分未配置、未登记、实例不存在。

## 工单

- 申请页数据源下拉只展示数据库地址，不再显示实例标签名。

## 打包与发版

```bash
STAMP=v1.12.78 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.78 -m "v1.12.78"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。须用本版包更新后端，并重下发 sql-agent 后，选表字段的远程加速才生效。
