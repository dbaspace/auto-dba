# KeePlus / auto-dba v1.12.64

相对 [v1.12.63](changelog-v1.12.63.md)。

## CDC 加表写父 Connector（对齐创建 init）

- **分表组 include.list**：按 mappings 写入 regex（如 `public.tbl_order_tax(_[0-9]+|[0-9]+)`），不再罗列物理分表名。
- **ByLogicalTableRouter**：`transforms` 链加入逻辑表名，并写入 type / topic.regex / topic.replacement / key.field.name 四字段。
- **普通表**：仍追加字面 `schema.table`；合并幂等，无变更不 PUT。
- **预览与执行**共用 `MergeAddTableConnectorConfig`，预览可见 regex 与 router。

## 打包与发版

```bash
STAMP=v1.12.64 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.64 -m "v1.12.64"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
