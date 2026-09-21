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
