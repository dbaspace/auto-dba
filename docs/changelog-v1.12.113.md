# KeePlus / auto-dba v1.12.113

相对 [v1.12.112](changelog-v1.12.112.md)。实例总览采集 StarRocks 的 FE、BE 和磁盘。SQL 审核把还没写完查询体的 `INSERT ... SELECT` 保持成一条语句。无 Agent 协议变更。无表结构变更。

## 实例总览

- StarRocks 按连接主机里的 FE 地址采集，多个地址用逗号或分号分隔，连上一个即可。角色列显示 FE、BE 存活数。
- 数据盘总大小、数据存储是各 BE 之和。数据使用率取单节点最高值，悬停可见该 BE。增加 Tablet 数。
- FE 失联、BE 失联、下线中记入异常。StarRocks 的汇总卡片改为 FE 异常、BE 异常。导出 CSV 带上磁盘、Tablet 和存活数。

## SQL 审核

- `INSERT` / `REPLACE` 还没出现顶层 `SELECT` 或 `VALUES` 时，后面的 `SELECT` 不再拆成另一条语句。列清单括号里的内容不算顶层。
- 已经写过 `VALUES` 或 `SELECT` 之后，再出现的 `SELECT` 仍拆开。
- `INSERT SELECT` 里子查询的 `VALUES` 不再当成插入行数。

## 打包与发版

```bash
STAMP=v1.12.113 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.113 -m "v1.12.113"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。实例总览和 SQL 审核要使用本版平台进程，请重启平台后再用。
