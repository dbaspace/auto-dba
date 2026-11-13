# KeePlus / auto-dba v1.12.128

相对 [v1.12.127](changelog-v1.12.127.md)。数据库查询在 HTTP 页面上可以再次运行，SQL 窗口增加注释和取消注释快捷键。无表结构变更，无 Agent 协议变更。

## 数据库查询

- 点「运行」或 `Ctrl/⌘+Enter` 不再因为 `crypto.randomUUID is not a function` 发不出请求。HTTP 页面没有该函数时，改用 `crypto.getRandomValues` 生成查询请求号，查询审计和超时补记仍能对上。
- SQL 窗口：`Ctrl+/`（Mac 为 `⌘+/`）给当前行或选中的每一行加上 `-- `；`Ctrl+Shift+/`（Mac 为 `⌘+Shift+/`）去掉一层行注释。空行不动，可用撤销撤回。

## 打包与发版

```bash
STAMP=v1.12.128 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.128 -m "v1.12.128"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议。已经用 v1.12.124 及以后包下发过的 SQL 执行器不必为这个版本重装；若执行器仍是 v1.12.123 及更早，须用 v1.12.124 或本版包重下发。查询运行和注释快捷键要使用本版前端，请刷新页面。
