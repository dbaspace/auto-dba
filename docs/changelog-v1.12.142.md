# KeePlus / auto-dba v1.12.142

相对 [v1.12.141](changelog-v1.12.141.md)。实例总览的智能分析除磁盘增长外，补上同一窗口的主机负载。PostgreSQL DDL 工单可以提交带标签美元引号的修复脚本。无 Agent 协议变更。

## 主机智能分析

- 分析窗口内增加 CPU、磁盘 IO util、网络入站和出站、系统 TCP 连接数，各有均值和峰值。磁盘增长结论保持原样。
- CPU 峰值不低于 85%，或 IO util 峰值不低于 80% 时，建议列会标出。单项 Prometheus 查询失败只写入结论，不让整份报告失败。
- 报告标题统一为智能分析报告。查询超时由 90 秒改为 3 分钟。
- 登录会话的使用状态可以按使用中、离开排序。

## PostgreSQL DDL 审核

- `SET LOCAL search_path` 与 `lock_timeout`、`statement_timeout` 一样，必须写在 `BEGIN` 和 `COMMIT` 之间。
- `DO $标签$ ... $标签$` 里的 `CREATE`、`ALTER`、`COMMENT` 按原有 DDL 规则检查。`DECLARE`、`BEGIN`、`IF`、`SELECT`、`RAISE` 和变量声明可以留在块里。
- 块内出现 `INSERT`、`UPDATE`、`DELETE`、`MERGE`、`EXECUTE`、`CALL` 仍拒绝。MySQL 工单里的 `DO` 仍拒绝。
- 语句切分和注释剥离识别带标签的美元引号，不再只认 `$$`。

## 打包与发版

```bash
STAMP=v1.12.142 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.142 -m "v1.12.142"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议。已经用 v1.12.124 及以后包下发过的 SQL 执行器不必为这个版本重装；若执行器仍是 v1.12.123 及更早，须用 v1.12.124 或本版包重下发。智能分析和 DDL 审核要使用本版平台进程，请重启平台。
