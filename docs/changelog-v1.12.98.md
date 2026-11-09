# KeePlus / auto-dba v1.12.98

相对 [v1.12.97](changelog-v1.12.97.md)。Agent管理的 SQL 执行器页签把登记列表放到下发记录前面。无 Agent 协议变更，无表结构变更。

## Agent 管理

- SQL 执行器页签的顺序改为：下发、登记列表、下发记录。登记时可以直接选到上方刚成功的下发记录，不用先翻过下发记录表。

## 打包与发版

```bash
STAMP=v1.12.98 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.98 -m "v1.12.98"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。只改了页面，刷新即可，平台进程不必重启。
