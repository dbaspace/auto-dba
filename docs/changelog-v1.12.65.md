# KeePlus / auto-dba v1.12.65

相对 [v1.12.64](changelog-v1.12.64.md)。

## 工单库资源可见性

- **跨租 domain / instance 授权**：工单选库与 `GET /databases` 自动展开授权方（或实例归属）同实例已登记库；提交校验与列表一致，无需再单独授库。
- **本租划拨域名**：拥有指向外租实例的域名时，同样展开该实例归属租户下的已登记库。
- **前端空 domains**：库未绑定域名时 DDL/DML/DCL 不再滤空（与后端空 domains 放行一致）；有绑定仍须命中所选数据源。
- 本租自有 private 库仍走 ACL，展开路径不绕过 `VisibleTo`。

## 打包与发版

```bash
STAMP=v1.12.65 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.65 -m "v1.12.65"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
