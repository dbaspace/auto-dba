# KeePlus / auto-dba v1.12.67

相对 [v1.12.66](changelog-v1.12.66.md)。

## 跨租 / 划拨域名后库可见

- **实例列表**：`domain` grant 与本租外指域名的父实例并入 `GET /instances`，已登记库 Tab 能解析引擎类型。
- **查询对齐**：`GetDatabaseVisible` / 查询认 domain/instance 展开，与 List 一致。
- **划拨域名缺口**：本租已拥有域名（实例也可能已在本租）但库仍在异租时，工单/已登记库展开这些外租已登记库，避免 DML/DDL 库下拉为空。
- **前端**：缺父实例归入「其他」Tab；同实例多域名滤空时回退展示；已登记库页提示跨租入口在租户管理。

## 打包与发版

```bash
STAMP=v1.12.67 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.67 -m "v1.12.67"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
