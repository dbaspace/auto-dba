# KeePlus / auto-dba v1.12.41

相对 [v1.12.40](changelog-v1.12.40.md)。启动时若 `tbl_issues` 没有 `exec_progress` 列则追加 `TEXT`，已有列不改。工单新增状态 `stopped`（已终止）。

## 工单执行

- DDL 执行中把进度写到工单上，列表和详情轮询展示。统计分库时进度不确定；单库按语句，多库按已结束库数，并把当前库的语句比例算进去。进度里带当前实例、库和数据库会话号。
- 执行中可终止。审核人、admin 或 DBA 租户成员可点终止。`POST /api/v1/tickets/:id/stop` 会结束当前 PostgreSQL / MySQL / StarRocks / Redis 会话，工单标为已终止。归档、迁移、CDC、闪回会一并停掉对应任务。
- 某个库失败后仍继续其余目标，与 v1.12.40 相同。

## 主机同步

- 「从监控同步」按监控标签 `idc` / `env` / `environment` 或采集器名匹配环境，并带上该环境绑定的区域。新建主机写入区域、环境和标签。已有主机只补空的区域、环境和标签，不覆盖已填项。结果里区分新建、补齐、跳过和未能匹配环境。

## 打包与发版

```bash
STAMP=v1.12.41 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.41
```

部署见 [deploy.md](deploy.md)。
