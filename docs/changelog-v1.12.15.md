# KeePlus / auto-dba v1.12.15

相对 [v1.12.14](changelog-v1.12.14.md)。

## 告警与 StarRocks

- 告警只打已登记资源，并按规则类型过滤，避免未登记主机收到 PostgreSQL 通知。
- StarRocks BE 数据目录按填写或探测值原样写入，不再拼接 `be/storage`。

## SQL 审核

- `WITH` 与后续 `UPDATE`/`DELETE` 视为一条 DML，不再误拆；`FROM` 只引用本语句 CTE 时不报多表。
- 「禁止 WHERE 写 1=1」改为独立开关 `DMLForbidTautology`，可单独关闭。只在顶层 WHERE 的某个条件整体等于 `1=1` 时命中，`column = true`、`id = 11` 不再误报。

## 打包与发版

```bash
STAMP=v1.12.15 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.15
```

部署见 [deploy.md](deploy.md)。
