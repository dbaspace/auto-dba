# KeePlus / auto-dba v2.3.0

相对 [v2.2.0](https://github.com/dbaspace/auto-dba/releases/tag/v2.2.0) 的更新点。

## 域名管理（全引擎统一）

- 新增资源表 `tbl_access_domains`：`UNIQUE(domain, db_type)`，按实例类型落库，**禁止**把 MySQL 域名写到 PG（或其它引擎）行
- 菜单「资源配置 → 域名管理」`/config/domains`：按类型 Tab 筛选、一键同步、审计下单条 / **批量指定租户**
- 同步规则：新行归属**默认租户**；已存在（同 domain + 同 db_type）只更新 instance/env，不覆盖 `tenant_id`；可按当前类型过滤同步
- 实例创建/更新自动增量同步；实例划拨时同归属域名一并迁移
- 跨租授权支持 `resource_type=domain`
- 权限：`um.sidebar.config-domains`、`um.action.domains.sync` / `assign` / `edit`
- 启动引导：`schema_version` / `seed_version` 已递增；生产部署新包并重启即可建表、回填与补菜单（自定义角色需在角色授权勾选）

## 实例审计划拨

- 实例页审计中改为「指定租户」（可选目标租户），对齐已登记库；不再仅在「归属≠当前租户」时显示「划拨到当前租户」
- MySQL / PostgreSQL / StarRocks 等共用页，一次生效

## Redis 查询

- 侧栏改为 **环境 → 域名 → 逻辑库**；优先读域名表（`db_type=redis`），未同步时回退实例 `domains`
- 多域名连接与解析加固（见 `internal/redisquery`）

## 其它

- CDC 分库匹配等相关小修（随本迭代一并带上）

部署与下载见 [deploy.md](deploy.md)。多租户划拨说明见 [tenancy.md](tenancy.md)。
