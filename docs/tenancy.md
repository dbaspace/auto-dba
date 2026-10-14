# 多租户（行级隔离）

KeePlus 采用共享部署 + 元库行级 `tenant_id`。默认严格隔离；跨租户仅成员加入、资源授权（grant）或平台审计（impersonate）。

侧栏功能入口与页面说明见 [features.md](features.md#多租户)。日常操作步骤见 [ops-manual.md](ops-manual.md#3-多租户使用)。生产配置拉取见 [nacos-config.md](nacos-config.md)（与租户无关：Nacos Namespace 是平台部署，不是业务租户）。

## 模型

| 概念 | 说明 |
|------|------|
| `tbl_tenants` | 租户实体（code 唯一） |
| `tbl_tenant_members` | 用户↔租户成员 + 租户内 `role_ids` |
| `tbl_tenant_grants` | 跨租户资源授权（实例 / 已登记库 / 域名 / SR 集群 / 部署集群读路径已接） |
| `tbl_access_domains` | 访问域名资源（自实例 `domains` 同步；`UNIQUE(domain, db_type)`；可独立划拨） |
| `tbl_tenant_audit_logs` | 切租户 / 成员 / grant / impersonate 审计 |
| JWT `tid` / `tcode` | 活跃租户；`imp` 表示平台审计进入 |
| `X-Tenant-Id` | 前端随请求携带，仅校验与 JWT 一致，不能覆盖 |
| `plan` 套餐 | 推荐 `free` / `standard` / `enterprise`，亦可自定义（小写 `[a-z0-9._-]`，≤64；配额未接） |

用户是平台级账号；业务权限看当前 `tid` 下成员 `role_ids`。平台 URN（租户治理 / 用户 / 菜单 / LDAP）走全局角色。

### 菜单与角色授权

- 侧栏 / `GET /auth/me` / `GET /menus/mine`：按**当前租户成员** `role_ids` → `tbl_role_menus` 出树（`MenusForUserInTenant`）。
- seed 自动灌菜单（`GrantMenuToAllRoles`）**仅**针对内置角色 code：`admin` / `operator` / `viewer` / `developer` / `dba` 等；自定义角色（如 `pos_read`）只认「角色授权」显式勾选，不会被 seed 再次灌满。
- 前端按钮：业务 URN 只用 `permissions`（租户），平台 URN 只用 `platform_permissions`。
- 成员绑了多个角色时菜单为并集；若期望仅「pos项目」，请在租户成员里只保留该角色。

### 业务 ACL / 审批人（严格租户）

实例（含 host 私有可见）、已登记库的 `access_users` / `approvers`，以及工单/运维的 `assigned_to`（建库、DDL/DML/DQL、归档、部署、SR、HA、CDC、Todo 指派等）：

- **候选项**：`GET /tenant-acl-options`（及已收紧的 `/create-db/approvers`、`/tickets/target-approvers`、`/todos/users`）仅返回该租户活跃成员；角色为成员 `role_ids` 并集。
- **写入校验**：用户名必须是资源所属租户（或当前 `tid`）的活跃成员，否则 400。
- **可见性匹配**：`ViewerOfInTenant` 用租户成员角色与 `role_ids` 求交；平台 privileged（admin/dba/operator 等）仍可旁路 private。
- **审计编辑外租户资源**：候选项与校验按**资源所属 tid**，不是当前审计租户。
- **PLATFORM 例外**：用户/角色治理页、租户「添加成员」仍用全站用户列表。

平台管理员可管租户生命周期，**默认不自动**看见各租户业务数据；代管入口为 `um.action.tenants.impersonate`（进入租户审计）。

### 平台 Admin 审计代管 + 划拨

JWT `imp=true` 且用户为平台管理员时：

- 实例 / 已登记库 **List** 为全平台视野，响应带 `tenant_id` / `tenant_name`。
- 可 **划拨** 实例：`POST /api/v1/instances/:id/assign-tenant`（body `tenant_id` 指定目标，默认当前 `tid`）；下属同归属已登记库、账号与**域名资源**一并更新。带 `sr-cluster=` / `ha-from-dbdeploy` 标签时，同步划拨所属部署集群。前端审计页为「指定租户」弹窗（可选任意目标租户）。
- 可 **划拨已登记库**：`POST /api/v1/databases/:id/assign-tenant`（body `tenant_id`）。只改库与该库账号归属，**不改实例**。批量：`POST /api/v1/databases/assign-tenant`（`tenant_id` + `ids`）。
- 可 **划拨域名**：`POST /api/v1/access-domains/:id/assign-tenant`；批量：`POST /api/v1/access-domains/assign-tenant`（`tenant_id` + `ids`）。域名与引擎类型绑定（`UNIQUE(domain, db_type)`），同地址不同引擎互不影响。一键同步：`POST /api/v1/access-domains/sync`（可选 `db_type`）。跨租只读用 grant（`resource_type=domain`）。
- 可 **划拨集群**：`POST /api/v1/sr-cluster/clusters/:id/assign-tenant`、`POST /api/v1/db-deploy/clusters/:id/assign-tenant`（同样仅审计会话）；带走 tagged / linked 实例、库账号，以及 description 含 `sr-cluster=<id>` 的监控任务。Node Agent 不迁。
- 可 **划拨接入层代理**：`POST /api/v1/proxy/services/:id/assign-tenant`（HAProxy/Nginx/LVS）；目标租户内同 kind+name 或同 VIP 冲突则拒绝。
- 划拨前校验目标租户内：实例 `UNIQUE(tenant_id, domain_name, port)`；SR 同名或同 FE 入口；db-deploy **展示名可重复**，冲突看云 `endpoint_host:port` 与关联实例 domain+port；代理同 kind+name 或同 VIP。冲突返回明确错误。
- 普通成员仍严格按本租户过滤；跨租只读继续用 grant。

### 集群部署登记实例

- SR / db-deploy 集群 List/Get 带 `tenant_id`。
- 部署自动登记的资源实例（`EnsureClusterInstance` / `EnsureInstanceFromHostInTenant`）继承**集群租户**，禁止默落默认租户。
- 关联已有实例只匹配同租户（`ListInstancesInTenant` / `FindInstanceByHostPortInTenant`）；同 host:port 不同租户可各有一份。
- 启动时 `RealignDeployInstanceTenants`：按 `sr-cluster=<id>` / HA 节点把错位实例拉回集群租户。

## 成员归属

- 新建用户（含 LDAP/SSO）仅在**零有效成员**时加入默认租户。
- **普通用户**加入非 `default` 租户后，自动离开默认租户；启动时也会清理这类用户的 default 成员行。
- **平台管理员**可同时保留默认租户与业务租户成员（创建租户、顶栏切换依赖此能力）；登录/`/auth/me`/启动会自愈补回默认成员，且不可将平台管理员移出默认租户。
- 顶栏 `TenantSwitcher` 展示当前租户**名称**；删除当前会话租户后前端会切换到仍可用租户并刷新 Token（优先 `default`）。

## 资源隔离契约

TENANT 资源：List / Get / Create / Update / Delete / 测连 / 查询 / 发现均按当前 `tid`。

- Create 必须写入 `tenant_id`；空 tid **报错**（不静默落到 default）。
- 子资源默认继承父实例/集群 `tenant_id`。**已登记库例外**：库可单独指定 `tenant_id`（业务 owner），与实例基础设施租户不同；工单/查询在「拥有该库」或 `database` grant 时可解析父实例。实例划拨不覆盖已属其它租户的库。账号随库走。这是目录归属，不是引擎隔离。
- by-id 跨租户统一 **404**（不暴露存在性）。
- 实例唯一约束：`UNIQUE(tenant_id, domain_name, port)`；不同租户可登记相同私网地址。
- 测连 / 数据查询先按租户取实例（或库归属解析父实例）再解密密码；解密失败与「指定库租户」无关——划拨只改库/`tenant_id`，不写实例密码。常见原因是当前服务加密密钥与当初写入不一致；到「资源配置 → 实例」重填密码即可。

PLATFORM 资源（全局共享）：用户/角色/菜单、LDAP/SSO/SMTP、区域/环境/制品目录。

## API

- `POST /auth/login`：可带 `tenant_id`；多租户时返回 `need_tenant_select`
- `POST /auth/select-tenant` / `POST /tenants/switch`
- `GET /tenants/mine`、租户 CRUD（含 `DELETE /tenants/:id`：无成员且无绑定资源才可删；否则 409「需要先释放资源」/「请先移除全部成员」）、成员、授权、审计、impersonate
- `GET /tenants/:id/grant-resources?type=`：跨租户授权弹窗按类型列出资源方租户下的实例/库/域名/集群（需 `um.action.tenants.grants`；`type=domain`）
- `GET /access-domains`、`POST /access-domains/sync`：域名列表与一键同步（sync 需 `um.action.domains.sync`）
- `POST /instances/:id/assign-tenant`：平台审计会话下划拨实例归属（需 `um.action.tenants.impersonate`）
- `POST /databases/:id/assign-tenant`：指定库的单一归属租户（实例可留在原租户）
- `POST /databases/assign-tenant`：批量指定库归属（body `tenant_id` + `ids`；需审计会话）
- `POST /access-domains/:id/assign-tenant`、`POST /access-domains/assign-tenant`：单条/批量指定域名归属（需审计会话）
- `POST /sr-cluster/clusters/:id/assign-tenant`、`POST /db-deploy/clusters/:id/assign-tenant`、`POST /proxy/services/:id/assign-tenant`：审计会话下划拨 StarRocks / MySQL·PG·Redis 等部署集群 / HAProxy·Nginx·LVS

前端：请求带 `X-Tenant-Id`；无活跃租户进入业务页会跳转 `/select-tenant`。审计中实例/库/域名页可用「指定租户」或批量划拨。

## 防串租回归（自测清单）

1. 用户仅属「测试」租户时，顶栏不再出现「默认租户」。
2. 租户 A 建实例 `h:3306`，租户 B 可再建同地址；A 的 List/Get/测连看不到 B。
3. 知 B 的 instance/database/issue/todo/alert/cluster id，在 A 上下文请求 → 404。
4. 业务租户提交工单，行 `tenant_id` 为当前 tid，不进 default。
5. 告警规则/告警/静默按租户列表隔离；抑制 / 通知路由 / 值班排班为平台级共享（见 [alerts.md](alerts.md)）。
6. 跨租 grant 读实例/集群：被授权租户 Get 可见，未授权仍 404。
7. admin 进入「测试」审计：可见默认租户实例；划拨后实例归属「测试」，dba 成员可见，默认租户侧不再出现。
8. 跨租 Delete（账号 / Proxy / HA / SR / 部署集群 / 告警规则 / FE·DS·OSS）→ 404，目标行仍在。
9. 备份 Run / Agent 下发、部署变更、CMDB 关系/审计按租户 List；跨租 Get → 404。
10. HA 建集群成员不可挂其它租户实例；CDC schema/ops 不可解析其它租户任务。
11. 路由守卫 `handler.TenantResourceGuard`：SR / 集群部署 / HA 的 `clusters/:id/*`、`changes/:id/*`、`agents/:id/*`，HA `controllers/:id/*`，闪回/归档 `tasks/:id/*` 跨租一律 404（集群类 GET 允许资源授权）。
12. SR·部署 Agent、HA 控制器、Kafka 工单、复制槽创建时打租户（显式上下文优先，其次继承主机实例）；对应 List 按租户过滤。
13. 备份策略 删除/启停/立即执行、存储删除、连接器删除、静默取消、复制槽删除校验租户；告警/归档统计按租户；redis-rdb、pg-space 实例列表与按实例操作按租户过滤；迁移源/目标须为本租户实例。
14. OSS 配置 / DS 管理员 / FE 管理员 / CMDB 标签定义改为租户内唯一（`tenant_id + environment|key`）；DS 控制台会话缓存按租户隔离；CMDB 标签定义按租户（首次访问自动写入内置默认集）。
15. 告警事件、规则试算、静默影响数按租户；todo 已绑定图片校验 todo 归属；CDC 创建校验源/目标实例、父任务、Debezium 服务归属并写入租户；CDC 重新初始化候选按租户；主机引用检查只列本租户 Agent/节点。
16. 子资源（SR/部署节点与变更、HA 任务、归档执行记录）写入时继承父资源租户。
回归脚本：`scripts/_iso_ten_tests.sh`（20 项，需临时启动后端）。

## 边界

控制面 JWT/SSH/加密密钥仍为平台级共享信任根。行级方案满足业务数据隔离；进程级硬隔离请独立部署。
套餐配额、每租户自定义角色包不在本期。
