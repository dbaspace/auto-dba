# KeePlus / auto-dba v1.12.53

相对 [v1.12.52](changelog-v1.12.52.md)。

## 已登记库

- 新增「批量撤销审核人」：对勾选库做差集（`POST /databases/batch-remove-approvers`）；某库没有所选用户则跳过；可撤空（工单回退实例审批人）。
- 撤销候选人取勾选库名单并集，含非本租户成员，便于清掉划拨残留孤儿名。

## 打包与发版

```bash
STAMP=v1.12.53 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.53
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
