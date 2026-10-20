# KeePlus / auto-dba v1.12.26

相对 [v1.12.25](changelog-v1.12.25.md)。启动时幂等创建 `tbl_sync_slot_op_logs`（采集/重启操作日志），无需单独执行 SQL。

## 同步列表（复制槽）

- **批量重启对齐 Hub**：勾选后确认，异步执行——先对业务库 `UPDATE tbl_heartbeat_table`，再 `POST .../connectors/{name}/tasks/0/restart`（仅 task 0，不走 CDC 任务页 includeTasks 全量重启）。不删除 PostgreSQL 复制槽。
- 重启提交用稳定键 `items[{host,port,slot_name}]`（兼容旧 `ids`）；页面展示重启进度（`GET /sync-slots/restart`）。
- 采集按 `(host/alias, port, slot_name)` **原地更新保留 id**，避免重启因 id 轮换被跳过。
- 采集与重启互斥；操作日志写入 `tbl_sync_slot_op_logs`，同页「查看操作日志」可查（`GET /sync-slots/logs`）。

## 打包与发版

```bash
STAMP=v1.12.26 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.26
```

部署见 [deploy.md](deploy.md)。
