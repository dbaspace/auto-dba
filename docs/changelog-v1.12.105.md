# KeePlus / auto-dba v1.12.105

相对 [v1.12.104](changelog-v1.12.104.md)。PAT 查询在 StarRocks 上可以执行一批只读 `SHOW`。账号、权限和导出类语句仍然拒绝。无 Agent 协议变更，无表结构变更。浏览器里的数据库查询不走这套限制。

## PAT 查询

- 所有引擎仍可执行 `SHOW TABLES`、`SHOW DATABASES`、`SHOW SCHEMAS`、`SHOW COLUMNS`、`SHOW FIELDS`、`SHOW INDEX`、`SHOW KEYS`、`SHOW CREATE TABLE`、`SHOW CREATE VIEW`。
- 目标库是 StarRocks 时，还可以执行 `SHOW TABLET`、`SHOW PARTITIONS`、`SHOW DATA`、`SHOW BACKENDS`、`SHOW FRONTENDS`、`SHOW PROC`、`SHOW ROUTINE LOAD`、`SHOW LOAD` 等只读 `SHOW`。其它引擎执行这些语句会返回「仅 StarRocks 可以执行该 SHOW」。
- `SHOW GRANTS`、`SHOW CREATE USER`、`SHOW USERS`、`SHOW ROLES`、`SHOW PROPERTY`、`SHOW AUTHENTICATION`、`SHOW REPOSITORIES`、`SHOW STORAGE VOLUMES`、`SHOW WHITELIST`、`SHOW SQLBLACKLIST` 仍然拒绝。`SHOW` 里出现 `INTO`、`OUTFILE`、`DUMPFILE` 也拒绝。

## 打包与发版

```bash
STAMP=v1.12.105 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.105 -m "v1.12.105"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。PAT 的 StarRocks `SHOW` 要使用本版平台进程，请重启平台后再用。
