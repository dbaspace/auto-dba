# KeePlus / auto-dba v1.12.46

相对 [v1.12.45](changelog-v1.12.45.md)。

## 修复

- **PostgreSQL DML 卡在「后台执行中」、库无落库**：`MaxOpenConns(1)` 下 `BeginTx` 后误对 `*sql.DB` 查会话号，占死唯一连接，语句与 `COMMIT` 永不执行。改为在事务连接上取 `pg_backend_pid()`；本机与 sql-agent 同源修复。

## 执行器

- 配置中心执行器支持 **停止远端进程**、**重新下发**；删除时可先停远端（失败可用 `force=1` 仅删登记）。
- 列表带回关联主机；平台 Agent 页可跳转相关操作。

## 打包与发版

```bash
STAMP=v1.12.46 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.46
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
