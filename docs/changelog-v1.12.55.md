# KeePlus / auto-dba v1.12.55

相对 [v1.12.54](changelog-v1.12.54.md)。

## 已登记库 / 批量审核人

- 批量追加与撤销审核人改为有限并发（8），缩短大批量勾选时的耗时。
- 撤销时拆开历史脏数据中逗号粘连的审核人名（如 `"a, b"`），写后可正确减掉。
- 前端：批量追加/撤销超时后静默复核、未撤干净可二次提交；审计下增加按租户筛选。

## 打包与发版

```bash
STAMP=v1.12.55 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.55
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
