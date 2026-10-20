# KeePlus / auto-dba v1.12.19

相对 [v1.12.18](changelog-v1.12.18.md)。

## CDC 同步任务

- 运行列表只对**当前页**的初始化任务做有界并发 pipeline 探测，不挡首屏；翻页后按页重新探测。
- 列表接口不再同步探测 Connect；`can_reinit` 按任务状态规则返回。连接器缺失仍由详情 / 重新初始化候选接口标注。

## 工单与查询

- 工单列表增加申请原因、环境、数据库类型；DQL 工单状态 `executed` 展示为「已下载」。
- DQL 导出下载区分错误 JSON 与成功文件；已下载后可再次下载。PostgreSQL 导出设置会话 `statement_timeout`，避免慢查询拖死导出。
- 通用查询树主标题为库名，短名 / alias 放在 tag。

## 其它

- `.gitignore` 忽略 Hub CDC 迁移数据目录与生成 SQL（`scripts/cdc/`、`import_hub_cdc*.sql` 等），发版不携带迁移数据。

## 打包与发版

```bash
STAMP=v1.12.19 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.19
```

部署见 [deploy.md](deploy.md)。
