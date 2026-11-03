# KeePlus / auto-dba v1.12.74

相对 [v1.12.73](changelog-v1.12.73.md)。无表结构变更。

## CDC 批量加表：可加覆盖明细

- **缺表/占用名单**：`POST .../batch-add-table/available-tables` 每表返回 `missing` / `occupied`（父任务 id、名称、`source_db`）；`parents` 补充 `name` / `source_db` / `instance_name`。
- **前端「可加 N/M 父」**：Tooltip 标明各父按其 SourceDB 拉表，并列出缺表与已占用源库（默认前 12 条 + 总数）；分表组对 members 的缺表父做并集。
- 不改变「每父只查 SourceDB」口径，也不自动把覆盖数补到 64；以名单便于核对哪些库无表。

## 打包与发版

```bash
STAMP=v1.12.74 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.74 -m "v1.12.74"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。
