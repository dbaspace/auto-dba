# KeePlus / auto-dba v1.12.56

相对 [v1.12.55](changelog-v1.12.55.md)。

## 系统通知

- 配置中心「系统通知」：登录弹框模板（启停、每次登录 / 仅一次 / 版本变更）。
- 登录后按模式弹出；用户确认后按版本/模式记录，避免重复打扰。

## 多租户与跨租授权

- 跨租授权支持**批量授权 / 批量撤销**（单次最多 500）。
- 资源划拨时自动迁移 `tbl_tenant_grants`：`from` 改为新归属，目标已是新归属则删冗余；读路径要求 `from` 等于资源当前归属。
- 平台管理员「进入租户模式」与 `POST /tenants/exit-impersonate` 退出；换票世代号避免切换租户时误登出。

## 平台资源申请与工作台

- 划拨/跨租可见性审批人池：平台管理员 ∪ 超级审核白名单 ∪ **dba 租户成员**。
- 普通审批人可跨租查看 `assigned_to` 为自己的资源申请单；工作台统计对管理员/白名单/dba 聚合跨租数据。

## 打包与发版

```bash
STAMP=v1.12.56 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.56
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
