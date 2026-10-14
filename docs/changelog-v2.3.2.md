# KeePlus / auto-dba v2.3.2

相对 [v2.3.1](changelog-v2.3.1.md)（若尚未拉取该提交，本包已包含其全部能力）的功能与体验更新。

## 工单数据源改走域名管理

- DDL / DML / 建库 / 权限申请的「数据源」下拉改为 `GET /access-domains`：仅展示**当前租户可见**的域名（本租户归属 ∪ `resource_type=domain` 授权）。
- 不再按实例 `tenant_id` 展开 `domains[]` 决定地址是否出现。
- `AccessDomain` List 补充 `instance_name`，下拉右侧展示实例别名。
- 提交时校验：域名属于所选实例登记，且在域名管理中对本租户可见（平台管理员审计会话放宽）。

## 登录页记忆认证账号

- 按认证方式（平台 / LDAP / SSO）分别记住上次用户名。
- 选择 LDAP 时不再预填平台演示账号 `admin`；密码不落本地。

## 说明

- 部署前请先在「域名管理」一键同步，否则未登记域名提交工单会提示先同步。
- 功能说明见 [tenancy.md](tenancy.md)、[features.md](features.md)、[deploy.md](deploy.md)。
