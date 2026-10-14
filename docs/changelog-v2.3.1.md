# KeePlus / auto-dba v2.3.1

相对 [v2.3.0](changelog-v2.3.0.md)（若尚未拉取该提交，本包已包含其全部能力）的补丁。

## 修复

- **域名管理划拨 404**：单条「指定租户」改走 `POST /access-domains/:id/assign-tenant`；多条仍走 `POST /access-domains/assign-tenant`。避免前端一律打批量接口时，旧进程未注册批量路由导致 404。

## 说明

- 批量划拨仍需后端包含 v2.3.0 起的 `AssignTenantBatch` 路由（部署本包或重启新二进制即可）。
- 功能说明见 [changelog-v2.3.0.md](changelog-v2.3.0.md)、[tenancy.md](tenancy.md)、[deploy.md](deploy.md)。
