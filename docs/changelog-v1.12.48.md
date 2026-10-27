# KeePlus / auto-dba v1.12.48

相对 [v1.12.47](changelog-v1.12.47.md)。

## 修复

- **已有库升级后看不到 Nacos 侧栏**：`migrateFast` / `seedFast` 在 schema/seed 版本跳过时，幂等补齐 `tbl_nacos_admin` 与「Nacos 配置 / Nacos 管理」菜单，并把监控运维叶子统一挂到配置中心观测分组。

## 打包与发版

```bash
STAMP=v1.12.48 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.48
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
