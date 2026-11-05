# KeePlus / auto-dba v1.12.85

相对 [v1.12.84](changelog-v1.12.84.md)。查询页铺满屏幕，同名结果列分开显示，库目录首次和刷新都少等。无 Agent 协议变更。

## 数据库查询

- 查询页跟着窗口铺满，外层不再滚动，库页签不会被顶部页签盖住。库名在页签里垂直居中，加号紧跟在最后一个查询页签后面。搜索框左右空白收紧。
- 结果里两列同名时按列序号分别取值。`project.name` 和 `process.name` 不再显示成同一份数据，表头仍用库返回的列名。
- 登录后预取当前租户的库目录，并记在本次浏览器会话里。再次打开或刷新时先画出上次的库树，同时在后台更新。目录里的几路查询改为并行。启动时给 `tbl_databases(tenant_id)` 建索引 `idx_databases_tenant`。

## 打包与发版

```bash
STAMP=v1.12.85 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.85 -m "v1.12.85"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
