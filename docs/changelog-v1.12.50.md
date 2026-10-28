# KeePlus / auto-dba v1.12.50

相对 [v1.12.49](changelog-v1.12.49.md)。

## 租户与审核人

- 库划拨到目标租户时，清理非该租户活跃成员的审核人 / 可访问用户。
- 追加库审核人：保留现有合法成员并与本次提交合并；非成员不会写入。
- 前端按租户成员过滤审核人候选项（DDL / 库管理）。

## 打包与发版

```bash
STAMP=v1.12.50 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.50
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
