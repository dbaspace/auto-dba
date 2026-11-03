# KeePlus / auto-dba v1.12.72

相对 [v1.12.71](changelog-v1.12.71.md)。无表结构变更。

## CDC 加表「下一步」性能

- **synced-tables 不再连源库**：列表/勾选只用平台记录占用，去掉 64 父任务时对 publication 的重复 live 核对。
- **批量可选表瘦身**：`POST /cdc-sync/tasks/batch-add-table/available-tables` 每父单连接拉 schema + publication；响应不再下发全量 exists/occupied（保留 tables 聚合与 occupied_count）。多父下一步只等该接口，不再串行等 synced-tables。
- **异地就近（sql-agent）**：新增同步只读接口 `POST /v1/sql/meta`（同实例多库一次返回表清单与 publication 成员）。CDC 按实例 `idc` 分流：本机直连 / 走执行器 / 异地无执行器则提示部署、不硬跨网。不改动现有 `/v1/sql/jobs`、`/v1/sql/export-jobs`。
- **须用本版包重下发 sql-agent** 后，海外环境加表可选表才会走 meta；旧执行器 DDL/DQL 不受影响。

## 打包与发版

```bash
STAMP=v1.12.72 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.72 -m "v1.12.72"
```

部署见 [deploy.md](deploy.md)。一体包含更新后的 `deploy/sql-agent/bin/sql-agent`。
