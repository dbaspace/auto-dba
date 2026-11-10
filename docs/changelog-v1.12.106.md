# KeePlus / auto-dba v1.12.106

相对 [v1.12.105](changelog-v1.12.105.md)。查询窗口在库名点号后提示表名，写完的字段和空白处不再弹出候选。PAT 在 StarRocks 上放行其余只读 `SHOW` 和 `ADMIN SHOW`。无 Agent 协议变更，无表结构变更。

## 查询补全

- `FROM`、`JOIN` 和表清单逗号后面的 `库名.` 用已加载的表名做候选，只替换点号后面的文字。`WHERE`、`SELECT` 里的 `别名.` 或 `表名.` 仍只提示字段。
- 候选只在正在输入的名字后面、刚写完的点号后面，或当前语句同一行分号之前自动打开。点空白、空行、分号后面，以及已经写完整的字段，不再弹出。`Ctrl / ⌘ + Space` 仍可手动打开。

## PAT 查询

- 目标库是 StarRocks 时，除账号、权限和导出类语句外，其余 `SHOW` 都可以查询，包括 `SHOW SESSION VARIABLES`、`SHOW GLOBAL VARIABLES`、`SHOW STATS META`、`SHOW HISTOGRAM META`。
- `ADMIN SHOW` 可以查询，例如 `ADMIN SHOW CONFIG`、`ADMIN SHOW REPLICA STATUS`。`ADMIN SET` 会改配置，仍然拒绝。
- `SHOW GRANTS`、`SHOW CREATE USER`、`SHOW USERS`、`SHOW ROLES`、`SHOW PROPERTY`、`SHOW AUTHENTICATION`、`SHOW REPOSITORIES`、`SHOW STORAGE VOLUMES`、`SHOW WHITELIST`、`SHOW SQLBLACKLIST` 仍然拒绝。`SHOW` 或 `ADMIN SHOW` 里出现 `INTO`、`OUTFILE`、`DUMPFILE` 也拒绝。其它引擎执行 StarRocks 专有语句会返回「仅 StarRocks 可以执行该 SHOW」。

## 打包与发版

```bash
STAMP=v1.12.106 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.106 -m "v1.12.106"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。查询补全刷新页面即可。PAT 的 StarRocks `SHOW` 要使用本版平台进程，请重启平台后再用。
