# KeePlus / auto-dba v1.12.58

相对 [v1.12.57](changelog-v1.12.57.md)。

## DQL 异步导出

- 确认导出格式后立即返回：工单进入 `running`，平台后台按环境就近本机直连或 SQL 执行器查询并编码。
- 结果落盘平台临时目录 `data/dql-exports/`，再通过 `GET /tickets/:id/export/file` 下载；临时文件约保留 7 天，启动时清理过期目录。
- 详情页展示「下载流程」步骤与进度；本机路径跳过「下发执行器 / 回传平台」；失败可重导，无需重新审批。
- 环境未绑定执行器且不在 `ticket_local_idc` 时拒绝硬跨网直连。
- **无库表 DDL**：元数据写入现有 `need_rollback` / `exec_progress` JSON。

## SQL 执行器（sql-agent）

- 新增 `/v1/sql/export-jobs`（Submit / Get / File / Stop），远程环境就近导出后由平台回拉文件。
- 一体包、后端包与 Docker 镜像仍打包 `deploy/sql-agent/bin/sql-agent`；远程环境需用本版包 **重新下发** 执行器后才支持 DQL 远程导出。

## 其它

- 系统通知支持「发版冻结工单提交」（开关与自定义提示）；申请页在冻结期间拦截提交。
- SQL 审核拆分：抑制 `CREATE DATABASE` 后续选项行、`ALTER TABLE` 内 `DROP`/`ALTER COLUMN` 等被误判为新语句。

## 打包与发版

```bash
STAMP=v1.12.58 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.58
```

部署见 [deploy.md](deploy.md)。一体包 / 后端包含更新后的 `deploy/sql-agent/bin/sql-agent`。
