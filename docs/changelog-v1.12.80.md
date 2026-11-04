# KeePlus / auto-dba v1.12.80

相对 [v1.12.79](changelog-v1.12.79.md)。无表结构变更。无 Agent 协议变更。

## 工单

- DDL 数据源下拉的关键字只匹配域名。输入 `report` 时，不再因为实例名带这个字而列出 `dorisdb.us.restosuite.ai` 这类地址。

## 打包与发版

```bash
STAMP=v1.12.80 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.80 -m "v1.12.80"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
