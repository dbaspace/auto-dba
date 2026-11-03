# KeePlus / auto-dba v1.12.70

相对 [v1.12.69](changelog-v1.12.69.md)。无表结构变更。

## CDC 任务划拨到指定租户

- **整棵任务树一起划**：平台管理员在租户审计模式下，把初始化任务连同新增表、移除表、源表加列等子任务改归属到目标租户。传入子任务 ID 时回溯到根任务再划。
- **接口**：`POST /cdc-sync/tasks/:id/assign-tenant`、`POST /cdc-sync/tasks/assign-tenant`（单次最多 200）。仅 `tenants.impersonate` 且处于审计会话可用。
- **列表**：审计会话返回 `tenant_id` / `tenant_name`，页面可勾选初始化任务后划拨。非审计会话行为不变。
- **不影响链路**：只改任务 `tenant_id`。日志、配置版本按 `task_id` 跟随。Debezium、StarRocks 与编排不改。原 CDC 工单仍留在原租户。
- **实例可见性只提示**：目标租户看不到源端或目标实例时划拨仍成功，并提示需另行划拨或授权对应库/实例，否则详情里实例名会显示成地址。

## 打包与发版

```bash
STAMP=v1.12.70 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.70 -m "v1.12.70"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
