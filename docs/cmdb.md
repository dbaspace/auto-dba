# CMDB 模块

DBA 域配置管理（门面演进）：现有实例/库等表仍为写入真相源；本模块提供统一目录、关系拓扑、影响分析、标签、发现登记、导入导出、变更审计、快照漂移与外部同步。

## 原则

- 工单 / 查询 / CDC 等仍使用 `instance_id` / `database_id`，不改绑抽象 CI ID。
- 外部同步默认只产出**待确认候选**，不自动创建生产实例（全局参数 `cmdb_sync_auto_apply`，默认关）。
- 主机（`db_type=host`）被 Node / DB / Platform Agent 或集群节点引用时，`DELETE /instances/:id` 返回 409 并列出引用；须先下线节点、删除 Agent 再删主机。
- CI 变更可选 Webhook：全局参数 `cmdb_webhook_url`（空则关闭）。

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

- `GET /cmdb/cis?type=instance&q=` — 统一目录（含健康徽章 / app·biz 标签）
- `GET /cmdb/cis/:type/:id` — 详情
- `GET /cmdb/cis/:type/:id/graph?depth=1|2|3` — 拓扑
- `GET /cmdb/cis/:type/:id/impact?direction=up|down|both&depth=` — **影响分析**
- `GET|POST /cmdb/cis/:type/:id/snapshots` — CI 快照（`?baseline=1` 设基线）
- `GET /cmdb/drifts` — 相对基线漂移列表
- `GET /cmdb/by-app?tag=app=xxx` — 按业务标签聚合
- `GET|POST|PUT|DELETE /cmdb/tags` — 标签字典
- `PUT /cmdb/cis/:type/:id/tags` — CI 标签绑定（回写实例/库 JSON tags）
- `GET /cmdb/audit` — 审计
- `GET /cmdb/discover/from-monitor` — 监控候选（租户内实例匹配）
- `GET /cmdb/discover/from-clusters` — SR/部署/HA 节点候选
- `GET /cmdb/discover/health` — 数据质量概览
- `POST /cmdb/discover/databases/register` — 批量登记库
- `GET /cmdb/export` / `POST /cmdb/import` — 导入导出
- `/cmdb/sync/jobs` + `POST .../run` — 同步任务

## 权限 URN

- `um.sidebar.cmdb*` — 查看
- `um.action.cmdb.edit` — 编辑关系/标签/快照等
- `um.action.cmdb.import` — 导入导出
- `um.action.cmdb.sync` — 外部同步

## CI 类型

| 类型 | 说明 | 写入真相源 |
|------|------|------------|
| `instance` / `database` / `host` | 实例、已登记库、主机 | 业务表 |
| `sr_cluster` / `db_deploy_cluster` / `proxy_service` | 集群与代理（只读目录） | SR/部署/代理表 |
| `db_account` / `cdc_task` | 账号、CDC 任务（只读） | 账号/CDC 表 |
| `ha_cluster` / `backup_policy` | HA、备份策略 | 对应表 |
| `region` / `environment` / `connector` / `monitor_*` | 目录与监控 | 对应表 |

## 推导关系（节选）

- `instance --contains--> database` / `database --hosted_on--> instance`
- `instance --runs_on--> host` / `host --runs--> instance`
- `ha_cluster --manages--> instance`
- `sr_cluster|db_deploy_cluster --owns--> instance`
- `backup_policy --protects--> instance`
- `database --has_account--> db_account`
- `instance|cdc_task --data_flow--> …`（CDC 血缘轻量）
- 手工边：`depends_on` / `belongs_to_app`；业务标签约定 `app=` / `biz=`

## 影响分析

回答「改这个实例/库会影响谁」：

1. 拓扑页或资产详情点「影响分析」，或 `GET .../impact?direction=down`。
2. `direction=down` 汇总下游库/账号/备份/HA/CDC；`up` 看环境/主机/集群归属。
3. 活跃告警详情可点「查 CMDB 影响」（需告警 labels 含 `instance_id` 或 `ci_type`+`ci_id`）。

## 快照与漂移

- 表 `tbl_cmdb_ci_snapshots`：对 instance / database 关键属性（连接元数据、tags、visibility、approvers、tenant_id 等）做哈希快照。
- 更新实例/库时自动追加快照（内容未变则跳过）；首次自动标为基线。
- 发现页可「扫描漂移」；资产详情可手动拍快照 / 设基线。

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
- `tbl_cmdb_ci_snapshots`

## 能力路线图

| 阶段 | 目标 | 状态 |
|------|------|------|
| A | CI 类型扩展 + 推导关系 + 拓扑 depth=3 | 已落地 |
| B | impact API + 拓扑/资产/告警联动 | 已落地 |
| C | 发现源扩展、租户过滤、健康徽章 | 已落地 |
| D | CI 快照与关键属性漂移 | 已落地 |
| E | 业务标签聚合、Webhook、CDC data_flow、告警查影响 | 已落地 |

**明确不做（近两期）**：独立图数据库、替换现有业务表、表/列级全量元数据、AIOps 根因大模型。
