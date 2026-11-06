# KeePlus / auto-dba v1.12.89

相对 [v1.12.88](changelog-v1.12.88.md)。查询结果里超过 JavaScript 安全整数范围的大整数按十进制原文返回，避免订单号等末尾被改掉。无 Agent 协议变更，无表结构变更。

## 数据库查询

- `BIGINT`、`BIGINT UNSIGNED`、`LARGEINT`、`INT8`、`BIGSERIAL`、`SERIAL8`，以及 ClickHouse 的 `Int64`、`UInt64`、`Int128`、`Int256` 按十进制字符串返回。
- 普通 `INT`、`SMALLINT` 和不超过安全范围的无类型整数仍是数字。ClickHouse 的浮点和文本不变。

## 打包与发版

```bash
STAMP=v1.12.89 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.89 -m "v1.12.89"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
