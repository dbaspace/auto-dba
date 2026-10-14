# KeePlus / auto-dba v2.3.3

相对 [v2.3.2](changelog-v2.3.2.md)（若尚未拉取该提交，本包已包含其全部能力）的补丁与扩展。

## 修复：域名划拨后 Redis 查询「实例不存在」

- `resolveInstanceInTenant` 支持经 `tbl_access_domains` 归属或 `domain` grant 解析父实例。
- 仅划拨域名、实例仍属其它租户时，Redis / Kafka 查询可正常拉逻辑库 / Topic。
- Redis 列库 / 执行在带 `domain` 参数时校验域名对本租户可见。

## Redis / Kafka 工单数据源与环境

- 地址下拉改走 `GET /access-domains`（按 `db_type` + 环境过滤），提交带 `domain` 并后端校验。
- 环境选项改走 `GET /environments` + `buildEnvOptions`，不再写死 test / prod。
- 权限申请页环境同步去硬编码。

## 说明

- 功能说明见 [tenancy.md](tenancy.md)、[features.md](features.md)、[deploy.md](deploy.md)。
