# 细粒度权限：新增页面与操作登记指南

平台权限采用 **菜单 = 权限目录** 模型：角色勾选菜单树中的「页面」与「操作」节点；前端按 URN 隐藏按钮，后端按 URN 校验写接口。

**原则：页面上出现的每个写操作 / 敏感操作，都必须在菜单管理中登记为「操作权限」节点，并完成前后端双控。** 未登记即视为未完成。

## 节点类型

| 类型 | 识别约定 | 用途 |
|------|----------|------|
| 页面入口 | 有 `path`，URN `um.sidebar.*` | 侧栏、进页、「查看」 |
| 操作权限 | `path` 为空，URN `um.action.{资源}.{动词}` | 不进侧栏；角色授权；控按钮与 API |
| 目录分组 | 无 path、非动作 | 侧栏折叠容器 |

内置种子通过 `ensureActionMenus` 写入；也可在「菜单管理」手工新建（类型选「操作权限」）。编辑已有节点时类型只读，不可一键改类型。

## K8s 容器管理（已种子）

目录 `m-k8s`（容器管理），页面 URN：`um.sidebar.k8s-*`；操作 URN：`um.action.k8s.*`（cluster/pod/workload/yaml/node/db 等）。详见 [k8s.md](k8s.md) 与 `ensureK8sMenus`。

## 新增页面清单（必做）

以「区域管理」为例：`/config/regions` + 新建 / 编辑 / 绑定环境 / 删除。

### 1. 登记页面菜单（查看）

在 seed 或菜单管理中创建：

- 类型：页面入口  
- path：如 `/config/regions`  
- URN：如 `um.sidebar.config-regions`  
- 挂到合适目录下（如「配置中心」）

前端路由、侧栏会依赖该 path。

### 2. 登记页面上的每个操作

每个按钮 / 敏感动作单独一条「操作权限」子节点，挂在该页面菜单下：

| 操作 | Title（带资源名） | URN 示例 |
|------|-------------------|----------|
| 新建 | 新建区域 | `um.action.regions.create` |
| 编辑 | 编辑区域 | `um.action.regions.edit` |
| 绑定环境 | 绑定环境 | `um.action.regions.bind` |
| 删除 | 删除区域 | `um.action.regions.delete` |

命名要求：

- Title 禁止裸「删除」「编辑」，须带资源名  
- `permission_desc` 写清边界  
- 标准后缀：`create` / `edit` / `delete`；特有动词可用 `bind` / `collect` / `run` / `stop` / `approve` 等  
- 「查看」不另建动作，用页面菜单本身  

代码侧（与种子一致，推荐）：

1. [`internal/store/perms.go`](../internal/store/perms.go) 增加常量，如 `URNRegionsBind`  
2. [`internal/store/seed.go`](../internal/store/seed.go) `ensureActionMenus` 增加一行  
3. 重启后端后 `reconcileRoleMenus` 会给 `admin` / `operator` 补授权；`viewer` 仅页面  

也可仅在「菜单管理」UI 新建操作节点，再补常量与前后端绑定（URN 必须与代码一致）。

### 3. 后端：写接口绑 URN

在 [`internal/router/router.go`](../internal/router/router.go)：

```go
secured.POST("/regions", requireURN(store.URNRegionsCreate), d.Region.Create)
secured.PUT("/regions/:id", requireURN(store.URNRegionsEdit), d.Region.Update)
secured.PUT("/regions/:id/envs", requireURN(store.URNRegionsBind), d.Region.PutEnvs)
secured.DELETE("/regions/:id", requireURN(store.URNRegionsDelete), d.Region.Delete)
```

规则：

- GET / 列表 / 详情：通常不绑动作 URN（靠页面菜单进页）  
- POST 创建 → `.create`  
- PUT/PATCH 修改 → `.edit`（或独立动词如 `.bind`）  
- DELETE / 不可逆清理 → `.delete`  
- `admin` 角色短路放行（中间件已处理）  

### 4. 前端：按钮门禁

在 [`frontend/src/hooks/usePermission.ts`](../frontend/src/hooks/usePermission.ts) 增加 `Perm.xxx`：

```ts
regions: {
  create: 'um.action.regions.create',
  edit: 'um.action.regions.edit',
  bind: 'um.action.regions.bind',
  delete: 'um.action.regions.delete',
},
```

页面中：

```tsx
import Auth from '../../components/Auth'
import { Perm } from '../../hooks/usePermission'

<Auth urn={Perm.regions.bind}>
  <Button type="link" onClick={openBind}>绑定环境</Button>
</Auth>
```

无权限时按钮不渲染（隐藏，不用 Disabled 占位）。

### 5. 角色授权

- 打开 **用户权限 → 角色授权**，勾选页面（查看）及所需操作  
- 保存后用户 **刷新页面**（或重新登录）使 `/auth/me` 的 `permissions` 生效  
- 自定义角色不会自动获得新动作；内置 `admin`/`operator` 由 reconcile 差集补齐  

## 验收清单（发版前）

- [ ] 菜单管理能看到页面节点，类型 Tag 为「页面」  
- [ ] 其下每个写操作都有「操作」节点，筛选「仅操作」能列出  
- [ ] 动作节点路径显示「不进侧栏」，侧栏无多余项  
- [ ] 角色可单独取消某一操作（如只取消「绑定环境」）  
- [ ] 无操作权：按钮消失；直调对应写 API 返回 403 中文提示  
- [ ] 有查看无写权：可进页、见列表，无新建/编辑/删除等  

## 用户多角色（权限并集）

用户可绑定 **多个角色**（表 `tbl_user_roles`）。解析规则：

| 维度 | 行为 |
|------|------|
| 菜单 / URN | 各角色授权的 **并集**（去重）；`/auth/me` 的 `permissions`、`menus` 按此返回 |
| 管理员短路 | **任一** 角色 code 为 `admin`（或同等特权码）即视为管理员，放行全部启用 URN |
| 主角色 | `tbl_users.role_id` / `role` 与 JWT 中的 `role` 仍保留，取 `role_ids[0]`，仅作展示与兼容；**鉴权以关联表 + DB 查询为准**，不以 JWT role 为唯一依据 |
| 资源可见性 | Viewer 携带用户全部 `role_ids`；与资源 `role_ids` **有交集**即可看见；任一角色 privileged 则特权可见 |

API：

- `POST/PUT /users` 传 `role_ids: string[]`（至少 1 个）；仅传旧字段 `role_id` 时归一为单元素数组  
- `/auth/me` 额外返回 `role_ids`

升级库时会把历史 `tbl_users.role_id` 回填进 `tbl_user_roles`，单角色用户行为不变。

## 常见遗漏

| 现象 | 原因 | 处理 |
|------|------|------|
| 页面有按钮，菜单树没有对应操作 | 只做了页面菜单 | 补 seed / 菜单管理操作节点 |
| 按钮藏了，接口仍可调 | 只做了前端 Auth | 路由补 `RequireURN` |
| 菜单有操作，按钮一直显示 | 未包 `Auth` 或 URN 不一致 | 对齐 `Perm` 与种子 URN |
| 开发者刷新后仍有权 | 角色仍勾选该动作或缓存未刷新 | 取消勾选并刷新 |
| 多角色用户缺某按钮 | 所绑角色均未勾选该操作 | 给任一角色补授权，或换绑含该权限的角色 |

## 相关代码

- 中间件：`internal/middleware/auth.go` → `RequireURN`（`UserHasURN`）  
- 用户角色：`internal/store/user_roles.go` → `ListUserRoleIDs` / `ReplaceUserRoles` / `ListUserURNs` / `MenusForUser`  
- 种子动作：`internal/store/seed.go` → `ensureActionMenus`  
- 类型识别 / Tag：`frontend/src/utils/menuKind.tsx`  
- 菜单管理 / 角色授权 / 用户：`frontend/src/views/config/Menus.tsx`、`Roles.tsx`、`Users.tsx`  

## 高可用服务维护（增量）

- 页面：`/ops/ha`，URN `um.sidebar.ops-ha`，菜单 ID `m-plat-ha`（挂「平台运维」）
- 写接口一期挂在页面权限下；后续可细分为 `um.action.ha.submit` / `approve` / `execute` / `controller` 并在路由上补 `RequireURN`

## 集群部署（增量）

- 侧栏显示名「集群部署」：菜单 ID `m-plat-sr-cluster`（沿用），Path `/ops/cluster-deploy`，URN `um.sidebar.ops-cluster-deploy`（原 `um.sidebar.ops-sr-cluster` 由 seed 迁移）
- 分引擎工作台（同 URN 页面权限）：`/ops/cluster-deploy/starrocks|mysql|postgres|redis|haproxy|nginx|lvs` 等（以 Hub `/db-deploy/engines` 为准）
- API：`/api/v1/sr-cluster/*`（StarRocks）、`/api/v1/db-deploy/*`（MySQL/PG/Redis·Codis）
- 详见 [db-cluster.md](db-cluster.md)、[redis-cluster.md](redis-cluster.md)、[sr-cluster.md](sr-cluster.md)
- Hub：`/ops/cluster-deploy`；StarRocks 工作台：`/ops/cluster-deploy/starrocks`（旧 `/ops/sr-cluster` 前端重定向）
- 写接口一期仍挂 `/api/v1/sr-cluster/*` 与页面权限；方案见 [sr-cluster.md](sr-cluster.md)

## 域名管理（增量）

- 页面：资源配置下 `m-cfg-domains` → `/config/domains`，URN `um.sidebar.config-domains`
- 操作权限（挂 `m-cfg-domains` 下）：

| 操作 | Title | URN |
|------|-------|-----|
| 一键同步 | 同步域名 | `um.action.domains.sync` |
| 划拨（菜单登记） | 划拨域名 | `um.action.domains.assign` |
| 编辑 | 编辑域名 | `um.action.domains.edit` |

- 后端：`POST /access-domains/sync` 绑 `um.action.domains.sync`；单条/批量划拨绑 `um.action.tenants.impersonate`（与实例/库划拨一致，须审计会话）
- 前端：`Perm.domains.sync` 控同步按钮；划拨按钮用 `Perm.tenants.impersonate`

## 数据备份（增量）

- 菜单树挂「数据安全」：`m-safe-bak` → policies / runs / settings（备份存储）
- 平台 Agent：配置中心 `m-cfg-agent` → `/config/agents`，URN `um.sidebar.config-agents`
- OSS 配置：配置中心 `m-cfg-oss` → `/config/oss`，URN `um.sidebar.config-oss`（页面标题「对象存储」，含配置与分片上传）
- 页面 URN：`um.sidebar.ops-backup*`；写接口一期挂页面权限；后续可拆 `um.action.backup.policy.edit` / `run` / `restore`

## SQL 洞察（增量）

- 页面：`/ops/sql-insight`，URN `um.sidebar.ops-sql-insight`，菜单 ID `m-diag-sql`（挂「性能诊断」）
- 设置页：`/config/sql-insight`，URN `um.sidebar.config-sql-insight`（壳上仅一个「设置」入口，按当前 Tab 深链 `?tab=`）
- 操作权限（挂 `m-diag-sql` 下）：

| 操作 | Title | URN |
|------|-------|-----|
| 手动采集活动快照 | 手动采集活动快照 | `um.action.sql-insight.collect` |
| 手动生成 AWR | 手动生成 AWR 报告 | `um.action.sql-insight.awr-generate` |
| 手动采集表空间 | 手动采集表空间 | `um.action.sql-insight.space-collect` |
| 手动智能分析 | 手动智能分析 | `um.action.sql-insight.analyze` |

- 后端：`POST /pg-activity-snapshots/collect`、`/awr-reports/generate*`、`/pg-space/collect*`、`/sql-insight/analysis/runs` 已绑对应 URN
- 设置保存走全局参数，按钮需 `um.action.args.edit`

## 空目录与未接入页面（侧栏）

- 侧栏菜单（`/auth/me` 的 `menus`）只去掉操作节点；没有子菜单的目录也保留，悬停弹层提示「暂无页面」。
- 菜单登记了页面 path、但前端没有对应组件时，打开显示占位页：页面标题、该页面下登记的操作按钮（无权限置灰）、「暂无数据」。占位页数据来自 `GET /api/v1/menus/page-actions?path=`。

## 待办跟进（增量）

- 页面：`/ops/todo`，URN `um.sidebar.ops-todo`；种子目录「待办跟进」（`sort_order=65`，告警管理之前），子页「待办看板」。
- 表：`tbl_todos`、`tbl_todo_logs`、`tbl_todo_subtasks`（`internal/store/todo.go`）、`tbl_todo_images`（`internal/store/todo_image.go`）。
- 操作权限（挂页面下）：

| 操作 | Title | URN |
|------|-------|-----|
| 新建待办 | 新建待办 | `um.action.todo.create` |
| 编辑待办 | 编辑待办 | `um.action.todo.edit` |
| 跟进待办（新增 / 编辑子任务、更新子任务进展、转交、闭环、重新打开） | 跟进待办 | `um.action.todo.follow` |
| 删除待办 | 删除待办 | `um.action.todo.delete` |

- 跟进类操作除 `um.action.todo.follow` 外，还要求是负责人、跟进人或发起人；拥有 `um.action.todo.edit` 者可跟进任意事项。
- 页签：我的待办（我负责、未闭环）、待我跟进（我是跟进人、未闭环）、我发起的（含已闭环）、团队待办（全部未闭环）、已闭环、超期预警（已超期或 48 小时内到期）。
- 状态落库为 `pending` 待处理 / `following` 跟进中 / `handover` 待交接 / `closed` 已闭环；「已超期」按计划完成时间实时计算，不落库。
- 子任务：一个事项可拆成多个子任务（名称、描述即具体动作、负责人、计划完成时间、状态 `pending` 待处理 / `doing` 进行中 / `done` 已完成、进展说明）。
  - 「更新进展」针对某个子任务，进展追加到跟进记录（`tbl_todo_logs.subtask_id` 关联子任务），不覆盖历史。
  - 事项的「最新进展」「下一步动作」由子任务自动汇总：最新进展取最近一次有进展的子任务，下一步动作为未完成子任务按顺序编号；不再手工填写。
  - 子任务首次有进展时，待处理 / 待交接的事项自动转为跟进中；存在未完成子任务时不能闭环；已闭环事项须重新打开后才能操作子任务。
  - 事项名称只能通过「编辑」（`um.action.todo.edit`）修改，子任务操作不改名称。
  - 接口：`POST /todos/:id/subtasks`、`PUT /todos/:id/subtasks/:sid`、`POST /todos/:id/subtasks/:sid/progress`（`um.action.todo.follow`，且需与事项相关或有编辑权限）；`DELETE /todos/:id/subtasks/:sid`（`um.action.todo.edit`）。
- 图片：事项（新建 / 编辑）、子任务（新增 / 编辑）、子任务进展、闭环四处可附图，存 PostgreSQL（BYTEA）。
  - 先上传后绑定：`POST /todos/images`（multipart `file`，需新建 / 跟进 / 编辑任一权限）返回图片 ID；保存时在请求体 `image_ids` 中带上，后端校验「本人上传且未绑定」后认领。编辑时 `image_ids` 表示该处最终图片列表，移除的会删除。
  - 进展、闭环的截图挂在对应跟进记录上（`scope=log`），删除子任务时只删子任务自身图片，历史记录里的截图保留。
  - `GET /todos/images/:iid` 查看图片（登录即可；未绑定图片仅上传者可见），响应带 `nosniff` 与 `Content-Security-Policy: default-src 'none'`。
  - 限制：按文件内容识别，仅 PNG / JPG / GIF / WebP（不收 SVG），单张 5MB，每处 9 张；上传后 24 小时未绑定的图片在下次上传时自动清理。

## 告警管理（增量）

- 目录：`m-alert`「告警管理」；能力与分期见 [alerts.md](alerts.md)。
- 页面与侧栏 URN：

| 页面 | Path | URN |
|------|------|-----|
| 活跃告警 | `/ops/alerts` | `um.sidebar.ops-alerts` |
| 历史告警 | `/ops/alerts/history` | `um.sidebar.ops-alerts-history` |
| 规则管理 | `/ops/alerts/rules` | `um.sidebar.ops-alerts-rules` |
| 静默管理 | `/ops/alerts/silences` | `um.sidebar.ops-alerts-silences` |
| 通知路由 | `/ops/alerts/routes` | `um.sidebar.ops-alerts-routes` |
| 抑制规则 | `/ops/alerts/inhibitions` | `um.sidebar.ops-alerts-inhibitions` |
| 值班排班 | `/ops/alerts/oncall` | `um.sidebar.ops-alerts-oncall` |

- 操作权限：

| 操作 | Title | URN | 挂载 |
|------|-------|-----|------|
| 认领 / 转派 / 处理中 | 认领告警 | `um.action.alert.claim` | 活跃告警 |
| 恢复 / 误报关闭 | 关闭告警 | `um.action.alert.resolve` | 活跃告警 |
| 新建 / 取消静默 | 静默告警 | `um.action.alert.silence` | 活跃告警 |
| 新建 / 编辑规则、抑制、路由、值班 | 编辑规则 | `um.action.alert.rule.edit` | 规则管理（写接口复用） |
| 删除规则、抑制、路由、值班 | 删除规则 | `um.action.alert.rule.delete` | 规则管理（写接口复用） |

- 「转待办」复用 `um.action.todo.create`，`source=alert`、`source_ref=告警 ID`。
- 外发渠道（Webhook / 企微 / 电话 / 邮件）在「认证配置 → 通知」由管理员配置，不单独挂菜单 URN。
