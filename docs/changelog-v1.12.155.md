# KeePlus / auto-dba v1.12.155

相对 [v1.12.154](changelog-v1.12.154.md)。PostgreSQL 的 DDL 工单不再允许没有 `SET LOCAL` 的顶层 `BEGIN` / `COMMIT`。

## 审核

- 顶层 `BEGIN`、`BEGIN TRANSACTION`、`BEGIN WORK`、`START TRANSACTION`、`COMMIT`、`ROLLBACK`、`ABORT`，在整段脚本没有 `SET LOCAL` 时审核失败。提示：DDL 工单不能显式开启事务，请去掉 BEGIN / COMMIT。
- 为 `SET LOCAL lock_timeout`、`statement_timeout` 或 `search_path` 配套的 `BEGIN` / `COMMIT` 仍可提交。`SET LOCAL` 写在事务外，或有 `BEGIN` 却没有 `COMMIT`，仍按原来的提示失败。
- 函数体和 `DO` 块里的 `BEGIN` 不算显式事务。MySQL 的 DDL 工单遇到 `BEGIN` 仍是「语句必须是 DDL」。
- DML 工单仍由平台包事务，这次不改。

## 打包与发版

```bash
STAMP=v1.12.155 ./scripts/pack_release.sh all
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，不必仅为本次重下发执行器。审核在平台侧，请重启平台。
