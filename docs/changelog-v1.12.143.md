# KeePlus / auto-dba v1.12.143

相对 [v1.12.142](changelog-v1.12.142.md)。建库、DDL、DML 的审核结果可以点开看完整 SQL。主机智能分析的峰值带上发生时间。PostgreSQL DDL 工单放行函数、视图等对象语句，建表规则不再被函数体里的字样误伤。无 Agent 协议变更。

## 工单审核结果

- 建库、DDL、DML 的检查表里，SQL 语句改为前 80 个字符加「查看」。超长语句点预览或「查看」打开全文。
- 样式跟在 SQL 预览组件上，工单详情页的展示方式不变。

## 主机智能分析

- CPU、IO util、网络入站的峰值悬停显示发生时间，格式为月-日 时:分。出站和连接数仍只显示数值。
- 报告去掉地址列。
- 峰值时间查不到时保留峰值数字，并在结论里写明峰值时间查询失败。

## PostgreSQL DDL 审核

- 建表规则只认语句开头的 `CREATE TABLE`。`TEMP`、`TEMPORARY`、`UNLOGGED`、`GLOBAL`、`LOCAL` 以及 `CREATE OR REPLACE` 仍算建表。函数体、注释里出现 `CREATE TABLE` 不再触发主键等建表规则。
- DDL 工单放行函数、存储过程、触发器、物化视图、操作符、类型、域、规则、序列、排序规则、转换、聚合、事件触发器、外部表、扩展、全文检索、发布、订阅。
- PostgreSQL DDL 工单放行 `SECURITY LABEL`。DML 工单仍拒绝。没有主键的 `CREATE TABLE` 仍不通过。

## 打包与发版

```bash
STAMP=v1.12.143 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.143 -m "v1.12.143"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议。已经用 v1.12.124 及以后包下发过的 SQL 执行器不必为这个版本重装；若执行器仍是 v1.12.123 及更早，须用 v1.12.124 或本版包重下发。智能分析和 DDL 审核要使用本版平台进程，请重启平台。
