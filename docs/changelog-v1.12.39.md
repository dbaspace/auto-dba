# KeePlus / auto-dba v1.12.39

相对 [v1.12.38](changelog-v1.12.38.md)。无表结构变更，无配置项变更。

## DDL 工单

- 勾选分库并填写前缀后，PostgreSQL 执行时连管理库 `postgres`，按前缀列出库名。前缀里的 `_`、`%`、`\` 按字面匹配。只保留「前缀 + 可选一个下划线 + 纯数字后缀」的库，例如前缀 `db_order` 命中 `db_order_0`、`db_order0`。
- 代表库仍须已登记。其余分库按前缀现查，不要求逐个登记。未勾选分库或前缀为空时，仍只在代表库执行。非 PostgreSQL 拒绝分库执行。某个库失败即停止。
- 重试按「实例 + 库」跳过此前已全部成功的库，不再把同一实例上未成功的分库一并跳过。
- 执行结果按库分组，汇总文案改为「共 N 个库」。

## 打包与发版

```bash
STAMP=v1.12.39 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.39
```

部署见 [deploy.md](deploy.md)。
