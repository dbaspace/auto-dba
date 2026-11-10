# KeePlus / auto-dba v1.12.108

相对 [v1.12.107](changelog-v1.12.107.md)。查询窗口在 `WHERE` 等关键字后面提示字段或表名。无 Agent 协议变更，无表结构变更。

## 查询补全

- 光标贴在 `WHERE`、`AND`、`OR`、`HAVING`、`ON`、`USING`、`SELECT`、`BY` 末尾，或只隔着同一行空格时，提示当前语句已出现的表的字段。`FROM`、`JOIN` 后面仍提示表名。
- 插入位置在光标，不替换已经写好的关键字。`db.table` 仍按最后一个点后面的表名取字段。
- 字段名已经写完、语句中间的空白、空行和分号后面仍然不打开。`Ctrl / ⌘ + Space` 仍可手动打开。

## 打包与发版

```bash
STAMP=v1.12.108 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.108 -m "v1.12.108"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。刷新页面即可。
