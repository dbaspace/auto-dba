# KeePlus / auto-dba v1.12.94

相对 [v1.12.93](changelog-v1.12.93.md)。查询页切换同名库时，相同环境名会带上域名以便区分。无 Agent 协议变更，无表结构变更。

## 查询

- 同一库名落在多个实例、环境名又相同时，环境标签和切换列表显示「环境 · 域名」。优先用实例标签 `domain=`，其次库域名、实例域名，都没有时用实例名。
- 环境名能单独区分时仍只显示环境名。标签过长时换行，不再被裁成一行。

## 打包与发版

```bash
STAMP=v1.12.94 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.94 -m "v1.12.94"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
