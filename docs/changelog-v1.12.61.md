# KeePlus / auto-dba v1.12.61

相对 [v1.12.60](changelog-v1.12.60.md)。

## DQL 导出 gzip 压缩

- 导出写盘统一为 `dql_{工单号}_{时间}.{csv|json|sql}.gz`；`export_bytes` 为压缩后大小。
- 本机与 sql-agent 共用压缩逻辑；远程「传回平台」与浏览器下载均走压缩体，减小跨区（如新加坡执行器）与下载体积。
- **须同步升级并重新部署 `sql-agent`**，否则旧执行器仍可能回传未压缩文件（平台本机导出路径不受影响）。

## 已登记库

- 类型 Tab 计数与顶部过滤栏（库名/审核人/环境/租户/域名）对齐，避免筛完后类型数字仍按全量显示。

## 打包与发版

```bash
STAMP=v1.12.61 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.61
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
