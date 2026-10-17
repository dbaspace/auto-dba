# KeePlus / auto-dba v1.12.9

相对 [v1.12.8](changelog-v1.12.8.md)。

## 已登记库

- 有库编辑权限即可勾选行；工具栏支持「批量设置审核人」「批量设置可访问用户」。
- 批量设置审核人：覆盖所选库的 `approvers`；空名单清空库级审核人（工单回退实例审核人）。
- 批量设置可访问用户：仅当所选全部为**私有**库时可用，覆盖 `access_users`；不改变可见范围与绑定角色。
- 勾选库须同一租户；审核人 / 可访问用户均须为该租户活跃成员。

接口：`POST /databases/batch-approvers`、`POST /databases/batch-access-users`。

## 打包与发版

```bash
STAMP=v1.12.9 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.9
```

部署见 [deploy.md](deploy.md)。
