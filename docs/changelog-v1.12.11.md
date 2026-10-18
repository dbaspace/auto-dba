# KeePlus / auto-dba v1.12.11

相对 [v1.12.10](changelog-v1.12.10.md)。

## Redis 工单

- 审批通过后自动后台执行（纳入 `ShouldAutoRun`），无需再点「立即执行」。
- 执行时按工单所选域名连接实例；域名不合法时直接失败并写回执行结果。
- 详情页流程节点在无定时预约时显示「已自动执行」。

说明：DCL 仍不自动连库执行（多库 GRANT SQL 需另改生成与拆分逻辑后再开）。

## 打包与发版

```bash
STAMP=v1.12.11 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.11
```

部署见 [deploy.md](deploy.md)。
