# KeePlus / auto-dba v1.12.12

相对 [v1.12.11](changelog-v1.12.11.md)。

## 告警通知

- 外发文案（纯文本 / 企微 markdown）补充主机与挂载目录：优先读标签 `hostname`/`instance`/`host`、`mountpoint`/`path`/`device`；无值则省略，不影响非磁盘类告警。

## 告警历史

- 历史列表按外发次数逐条展示：通知 2 次 → 历史 2 条「通知」；指标回落后再追加 1 条「恢复」，与通知行共用同一告警 ID 对应。
- 恢复外发通知不再单独占一行；状态列仍沿用父告警 handle，不改写为「自动恢复」。
- 页面增加「主机 / 目录」「时间」「第几次」列；统计改为「历史记录 N 条」。

## 打包与发版

```bash
STAMP=v1.12.12 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.12
```

部署见 [deploy.md](deploy.md)。
