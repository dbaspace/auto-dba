# KeePlus / auto-dba v1.12.86

相对 [v1.12.85](changelog-v1.12.85.md)。查询页的页签、收藏和历史按当前租户分开。无 Agent 协议变更。

## 数据库查询

- 查询页签、收藏和历史的浏览器存储按当前租户分开。切到别的租户后，不再看到上一份库名和 SQL。
- 收藏增加 `tenant_id`，列表和写入只针对当前租户。启动时按已登记库回填；对不上库的旧收藏不再出现。
- 查询审计增加 `tenant_id`。「我的历史」只返回当前租户。以前没有记下真实租户的旧记录不再进入这个列表。

启动时若缺列或缺索引，会执行：

```sql
ALTER TABLE tbl_query_favorites ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_query_fav_user_tenant ON tbl_query_favorites(username, tenant_id);
ALTER TABLE tbl_query_audits ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_query_audits_user_tenant ON tbl_query_audits(username, tenant_id);
```

## 打包与发版

```bash
STAMP=v1.12.86 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.86 -m "v1.12.86"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
