# KeePlus / auto-dba v1.12.68

相对 [v1.12.67](changelog-v1.12.67.md)。

## CDC 批量加表链路名与 Debezium 按库合并

- **创建时按父任务改写链路身份**：子任务不再沿用表单里第一个父任务的 `publication` / `slot` / `connector` / `topic_prefix` 等字段；`prepareCDCCreate` 用当前父任务覆盖。
- **编排以父任务为准**：加表/加列/移除表执行时链路身份优先取父任务；已错串名的失败子任务升级后点「重试」即可走对库、对 connector。
- **Debezium 按分库过滤**：`stepAddTableDebezium` 按 binding 用 `MappingAppliesToDB` / `TablesForShardDB` 只合并本库表与分表路由（regex include + ByLogicalTableRouter），避免写进其它库的 connector。
- **日志与配置版本**：PUT 后记录 `db` / `connector` / 新增 include / 分表路由，并写入 `VersionSourceAddTable` 配置历史。
- **前端**：批量确认计划逐行展示各父任务 Publication/Connector；多父任务时提示「按各父任务自动使用」。

## 打包与发版

```bash
STAMP=v1.12.68 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.68 -m "v1.12.68"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
