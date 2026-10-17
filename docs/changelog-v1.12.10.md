# KeePlus / auto-dba v1.12.10

相对 [v1.12.9](changelog-v1.12.9.md)。

## 已登记库

- 搜索行控件间距收紧；工具栏齿轮可自定义列表字段（偏好存浏览器本地）。
- 长文本列按列宽省略，悬停可看全文。
- 首屏并行拉取实例/参数/环境/库列表；ACL 选项改到打开编辑弹窗时再加载。
- 列表 enrich 一次加载全部实例，避免按实例 N+1 查询导致审计全量列表变慢。
- 顶栏切换 Tab 时页面保活，不再反复重新加载；F5 / 关 Tab 重开仍会刷新。

## 打包与发版

```bash
STAMP=v1.12.10 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.10
```

部署见 [deploy.md](deploy.md)。
