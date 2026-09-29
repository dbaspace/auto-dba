# CMDB 模块

DBA 域配置管理（门面演进）：现有实例/库等表仍为写入真相源；本模块提供统一目录、关系拓扑、标签、发现登记、导入导出、变更审计与外部同步。

## 原则

- 工单 / 查询 / CDC 等仍使用 `instance_id` / `database_id`，不改绑抽象 CI ID。
- 外部同步默认只产出**待确认候选**，不自动创建生产实例（全局参数预留 `cmdb_sync_auto_apply`，默认关）。
- 主机（`db_type=host`）被 Node / DB / Platform Agent 或集群节点引用时，`DELETE /instances/:id` 返回 409 并列出引用；须先下线节点、删除 Agent 再删主机。

## 菜单

顶栏 **CMDB**：

| 页面 | 路径 |
|------|------|
| 资产目录 | `/cmdb/assets` |
| 关系拓扑 | `/cmdb/topology` |
| 标签管理 | `/cmdb/tags`（原 `/config/tags` 已指向此处） |
| 发现登记 | `/cmdb/discover` |
| 导入导出 | `/cmdb/import-export` |
| 变更审计 | `/cmdb/audit` |
| 同步任务 | `/cmdb/sync` |

## API 前缀

`/api/v1/cmdb/*`

主要接口：

- `GET /cmdb/cis?type=instance&q=` — 统一目录
- `GET /cmdb/cis/:type/:id` — 详情
- `GET /cmdb/cis/:type/:id/graph?depth=1|2` — 拓扑
- `GET|POST|PUT|DELETE /cmdb/tags` — 标签字典
- `PUT /cmdb/cis/:type/:id/tags` — CI 标签绑定（回写实例/库 JSON tags）
- `GET /cmdb/audit` — 审计
- `GET /cmdb/discover/from-monitor` — 监控候选
- `POST /cmdb/discover/databases/register` — 批量登记库
- `GET /cmdb/export` / `POST /cmdb/import` — 导入导出
- `/cmdb/sync/jobs` + `POST .../run` — 同步任务

## 权限 URN

- `um.sidebar.cmdb*` — 查看
- `um.action.cmdb.edit` — 编辑关系/标签等
- `um.action.cmdb.import` — 导入导出
- `um.action.cmdb.sync` — 外部同步

## CI 类型

`instance` / `database` / `host` / `region` / `environment` / `connector` / `monitor_job` / `monitor_group` / `ha_cluster` / `backup_policy`

## 同步适配器

| adapter | 说明 |
|---------|------|
| `prometheus_labels` | 从监控目标登记扫描候选 |
| `cloud_tencent` | 校验本地腾讯云 `cloud_*` 标注 |
| `http_cmdb` | 通用企业 CMDB HTTP 拉取（配置 `base_url` / `path` / `headers`） |

## 表

- `tbl_cmdb_relations`
- `tbl_cmdb_tag_defs` / `tbl_cmdb_tag_bindings`
- `tbl_cmdb_audit_logs`
- `tbl_cmdb_sync_jobs` / `tbl_cmdb_sync_runs`
