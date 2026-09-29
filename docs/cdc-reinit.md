# CDC 同步任务「重新初始化」

## 1. 背景与语义

Debezium（Kafka Connect）服务被删除后重新部署，已有 CDC 任务在 Connect 上找不到 Connector。「重新初始化」按任务元数据（`tbl_cdc_tasks.description` 中的 extra）逐项补齐资源，**已存在的跳过**：

| 资源 | 已存在 | 不存在 |
| --- | --- | --- |
| PG Publication | 跳过；只补缺失的表 | 创建 |
| PG Replication Slot | 跳过，复用原位点 | 创建（从当前位点开始捕获，之前的变更会缺失，预览会告警） |
| Debezium Signal 表 | 确保在 Publication 内 | 创建并加入 Publication |
| Kafka Topic | 跳过 | 创建 |
| Debezium Connector | 按任务元数据**覆盖配置**（覆盖前保存版本快照，来源 `reinit`，可在「配置版本历史」恢复） | 创建 |
| StarRocks Routine Load（初始化已完成） | 暂停的执行 RESUME（可取消勾选）；运行中的跳过 | 不新建，只告警 |
| 初始化未完成的任务（编排曾在 pg / kafka / debezium / starrocks 步骤失败） | 自动建表模式：目标表与 Routine Load 都存在则跳过 | 自动建表模式：按原配置补建目标表与 Routine Load；手动建表或无目标端：只回填 `target_sql`，不连 StarRocks |

规则：

- **入口门禁**：只有 Connect 明确返回 404（Connector `NOT_FOUND`）的任务才显示「重新初始化」。Connect 不可达、Connector 为 FAILED / PAUSED 的任务不显示；配置有问题但 Connector 还在的，继续用「编辑配置 / 版本恢复」。后端执行时同样校验，例外是 `error_step=reinit` 的「重试」。
- **按原提交配置**：表映射取全部未删除（`status != deleted`）的映射；分库任务每个物理库一个 Publication / Slot / Connector；Connector 配置在任务保存的 `debezium_config` 模板基础上，由任务元数据覆盖 `database.*`、`publication.name`、`slot.name`、`table.include.list`、`snapshot.mode`，并从源实例解密注入真实密码。任务里保存的副本仍是脱敏密码，`snapshot.mode` 保持原值。
- **snapshot_mode**：不传沿用任务原值（原值为空用 `never`）；显式传 `never` / `initial` 只对本次下发生效，不写回任务。
- **Connect 地址**：优先取「配置 → 连接器」注册表里该服务最新的 `dbz_host`，其次是任务 extra 里固化的 `connect_base_url`，最后是 `cdc.debezium_url`。
- **权限**：不走审批。仅超级管理员、平台管理员（角色 `admin` / `super_admin` / `platform_admin` / `平台管理员`），或全局审核白名单（全局参数 `issue_global_approver_whitelist`）用户可操作，否则返回 403「仅超级管理员、平台管理员或全局审核白名单用户可重新初始化 CDC 任务」。
- **异步**：受理后任务状态变为 `reinitializing`（「重新初始化中」），后台执行（单任务超时 15 分钟），成功后 `running`；失败为 `failed`，`error_step=reinit`，`error_message` 以「重新初始化失败[步骤]：」开头。进程重启时卡在 `reinitializing` 的任务会被自动回收为失败，可重试。

## 2. 字段变更

- 任务状态新增 `reinitializing`。执行中禁止暂停 / 恢复 / 重启 / 重试。
- `GET /cdc-sync/tasks`、`GET /cdc-sync/tasks/:id`：新增 `can_reinit`、`connector_missing`（不含权限判断）。列表按 Connect 地址分组探测，每组一次 `GET /connectors`，结果缓存 20 秒，总预算 3 秒，探测不到时为 false。
- `GET /cdc-sync/tasks/:id/connector`：Connector 缺失时返回 200：

```json
{
  "data": {
    "task_id": "xxx",
    "name": "prod_order",
    "state": "NOT_FOUND",
    "connector_missing": true,
    "missing_connectors": ["prod_order"],
    "can_reinit": true,
    "connect_base_url": "http://connect:8083",
    "config": {},
    "warning": "Kafka Connect 上未找到该 Connector（可能 Debezium 服务已重建），可通过“重新初始化”补齐"
  }
}
```

  此时 `PUT /tasks/:id/connector` 与版本恢复会被拒绝（Connect 的 `PUT /config` 会用脱敏密码新建 Connector）。
- `GET /cdc-sync/tasks/:id/status`：Connector 404 时 `connectors[].state` / `connector.state` 为 `NOT_FOUND`，`unhealthy_reason` 为「Debezium Connector 缺失，需重新初始化」。
- `GET /cdc-sync/tasks/:id/connector/status`：同上，`state` / `aggregated_state` / `worst_state` 为 `NOT_FOUND`。
- Connector 配置版本新增来源 `reinit`。

## 3. 接口

请求体字段（预览与执行相同，均可为空）：

| 字段 | 说明 |
| --- | --- |
| `snapshot_mode` | 空：沿用任务原值；`never`：从位点续传；`initial`：Connect 无 offset 时全量重做。其他值报错 |
| `connector_id` | 可选，换绑到另一个 Debezium 服务（`GET /connectors` 的 id）。新服务的 Kafka 必须与任务一致，换绑会同步更新子任务 |
| `resume_routine_loads` | 是否恢复已暂停的 Routine Load，默认 true |

### 3.1 预览

`POST /api/v1/cdc-sync/tasks/:id/reinit/preview`，只读。

```json
{
  "data": {
    "task_id": "xxx",
    "task_name": "order_sync",
    "status": "running",
    "connector_id": "c1",
    "connect_base_url_old": "http://old-connect:8083",
    "connect_base_url_new": "http://new-connect:8083",
    "snapshot_mode": "never",
    "snapshot_mode_original": "never",
    "mapping_count": 12,
    "init_incomplete": false,
    "items": [
      { "resource": "publication", "name": "prod_order", "db_name": "order", "exists": true, "action": "skip", "message": "已存在且表齐全，跳过" },
      { "resource": "slot", "name": "prod_order", "db_name": "order", "exists": true, "action": "skip", "message": "已存在，复用位点（active=false）" },
      { "resource": "kafka_topic", "name": "prod_order.public.t_order", "exists": true, "action": "skip", "message": "已存在，跳过" },
      { "resource": "connector", "name": "prod_order", "db_name": "order", "exists": false, "action": "create", "message": "Connect 上不存在，将创建" },
      { "resource": "routine_load", "name": "t_order_rl", "exists": true, "action": "resume", "message": "已暂停，将执行 RESUME" }
    ],
    "warnings": [
      { "code": "slot_missing", "message": "slot xxx 不存在，新建后从当前位点开始捕获，此前的变更会缺失，可按需走 add_table 初始化补数" }
    ],
    "configs": [
      { "connector_name": "prod_order", "db_name": "order", "config": { "database.password": "********" } }
    ]
  }
}
```

- `items[].resource`：`publication` / `slot` / `kafka_topic` / `connector` / `routine_load` / `mapping`（`mapping` 只在手动建表模式且初始化未完成时出现）。
- `items[].action`：`skip` 跳过、`create` 创建、`update` 覆盖配置或补表、`resume` RESUME、`warn` 需人工处理、`unknown` 探测失败执行时再判断。
- `warnings[].code`：`slot_missing`、`snapshot_initial`、`connector_override`、`routine_load_missing`、`probe_failed`、`connector_rebind`。

### 3.2 执行

`POST /api/v1/cdc-sync/tasks/:id/reinit` → `{"data":{"task_id","task_name","accepted":true,"status":"reinitializing"}}`。

受理后轮询 `GET /tasks/:id`（3 秒一次）直到 `running` 或 `failed`。日志 `step=reinit`（消息带「[重新初始化]」前缀）记录每项资源的处理结果，发起人记录在 `step=manual`。

错误沿用现有 CDC 接口约定：HTTP 400（权限不足 403，任务不存在 404），`{"message": "..."}`。常见 `message`：

- `仅 running / paused / failed 状态可重新初始化，当前状态=...`
- `仅 init 主任务支持重新初始化`
- `Connector 在 Debezium Connect 上仍存在，无需重新初始化`
- `Debezium Connect 不可达（...），无法确认 Connector 缺失`
- `任务没有有效的表映射，无法重新初始化`
- `新 Debezium 服务的 Kafka（...）与任务 Kafka（...）不一致，不能换绑`
- `任务正在重新初始化，请勿重复提交`

### 3.3 失败后重试

`status=failed` 且 `error_step=reinit` 时，原有的 `POST /tasks/:id/retry` 按默认参数（快照沿用原值、恢复 Routine Load、沿用当前绑定）重新发起，返回 `{"ok":true,"data":{"task_id","task_name","accepted":true,"status":"reinitializing"}}`，同样需要上述权限，不校验 Connector 缺失。

### 3.4 批量：按 Debezium 服务

候选：`GET /api/v1/cdc-sync/tasks/reinit/candidates?connector_id=c1`

```json
{
  "data": {
    "connector_id": "c1",
    "connect_base_url": "http://new-connect:8083",
    "connect_reachable": true,
    "message": "",
    "list": [
      { "task_id": "t1", "task_name": "order_sync", "env": "prod", "status": "running",
        "connector_name": "prod_order", "connector_names": ["prod_order"], "source_db_name": "order",
        "connector_exists": false, "can_reinit": true }
    ]
  }
}
```

执行：`POST /api/v1/cdc-sync/tasks/reinit/batch`，body `{ "connector_id": "c1", "task_ids": ["t1"], "snapshot_mode": "", "resume_routine_loads": true }`。

- `task_ids` 不传时自动选择 `can_reinit=true` 且 Connector 缺失的任务；此时 Connect 不可达直接报错。
- 批量不支持换绑；受理的任务在后台串行执行。
- 返回 `{"data":{"accepted":1,"rejected":1,"list":[{"task_id","task_name","accepted","status","message"}]}}`。

## 4. 前端入口

- 任务列表操作列：`can_reinit=true` 时显示「重新初始化」；管理员在工具栏可见「按 Debezium 服务重新初始化」。列表存在 `reinitializing` 任务时每 3 秒刷新。
- 详情页头部：`can_reinit=true` 时显示按钮；`error_step=reinit` 时横幅提示可「重试」。
- 「Debezium 配置」页签：`connector_missing=true` 时黄色提示 + 「重新初始化」按钮，并禁用编辑与版本恢复。
- 全链路中 Connector `NOT_FOUND` 显示为「缺失」。

## 5. 相关改动：新建任务编排的 Debezium 步骤

审批通过后的初始化编排（pg → kafka → debezium → starrocks）与重新初始化共用同一个 Connector 配置构建器：

- 分库任务每个库各建一个 Connector（此前只建一个）。
- 下发真实密码（此前是 `***`），任务里保存的副本仍脱敏。
- 只要能解析出 Connect 地址（连接器注册表 / 任务覆盖 / `cdc.debezium_url`）就创建，不再要求 `cdc.enabled=true` 且 `cdc.debezium_url` 非空；Connector 名以 shard binding 为准，不再被 publication 名覆盖。
- Publication 已存在时仍会确保 Replication Slot 存在（此前会直接返回，slot 缺失）。
