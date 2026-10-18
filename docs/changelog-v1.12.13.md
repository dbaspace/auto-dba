# KeePlus / auto-dba v1.12.13

相对 [v1.12.12](changelog-v1.12.12.md)。

## 告警历史性能与体验

- 历史查询按下推时间窗（`since`/`until`）+ 服务端分页，不再一次拉全量事件；实测首页约 0.1～0.15s、约 18KB（相对全量约 1.7s / 5MB）。
- 「第几次」用 SQL 窗口函数按告警全生命周期累计；事件表增加 `created_at` 索引。
- 恢复行增加「关联通知」，按同一告警 ID 筛选对应通知行，可「返回全部」。
- 表格「状态 / 操作」列右侧固定；窄屏自动隐藏「持续 / 恢复时间 / 负责人」等次要列，避免右侧裁切。

## 打包与发版

```bash
STAMP=v1.12.13 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.13
```

部署见 [deploy.md](deploy.md)。
