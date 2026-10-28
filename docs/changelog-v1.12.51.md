# KeePlus / auto-dba v1.12.51

相对 [v1.12.50](changelog-v1.12.50.md)。

## 审核人

- 工单取库级审核人与前端对齐：同实例精确库名，分库按前缀/族前缀匹配，否则回退实例审批人。
- 批量追加库审核人：只更新 `approvers` 列、写后回读校验；单条失败自动重试一次；非本租户成员仍自动丢弃。

## 打包与发版

```bash
STAMP=v1.12.51 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.51
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
