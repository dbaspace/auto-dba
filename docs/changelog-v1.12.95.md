# KeePlus / auto-dba v1.12.95

相对 [v1.12.94](changelog-v1.12.94.md)。查询页切换同名库时，环境列表按名称排序。无 Agent 协议变更，无表结构变更。

## 查询

- 同一库名的环境切换列表按环境名排序，没有环境名的排在后面；环境名相同再按域名排序。
- 标签文案仍按上一版：环境名能单独区分时只显示环境名，相同时显示「环境 · 域名」。

## 打包与发版

```bash
STAMP=v1.12.95 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.95 -m "v1.12.95"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
