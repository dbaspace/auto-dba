# KeePlus / auto-dba v1.12.52

相对 [v1.12.51](changelog-v1.12.51.md)。

## 库与审核人

- 统一 `EffectiveDatabaseTenantID`：空库租户回退实例/会话租户，批量追加与单库更新审核人 ACL 一致。
- 批量追加审核人：最多 3 次尝试（含退避）；无可追加成员时仍写入以清理非法审核人。

## 域名管理

- 列表支持按租户筛选。

## 打包与发版

```bash
STAMP=v1.12.52 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.52
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
