# 告警管理

平台自建规则评估：规则与告警生命周期由 KeePlus 管理，指标查询复用全局 Prometheus（`awr_prometheus_base_url`）。外发复用「用户权限 → 认证配置 → 通知」渠道，并可按路由/值班覆盖。

设计对齐 Alertmanager 思路（规则评估、静默、抑制、路由分组），但控制面与 UI 在本平台内闭环。

## 分期总览

| 期 | 状态 | 内容 |
|----|------|------|
| **一期** | 已完成 | 规则 CRUD、作用域 / `matcher_key`、多档阈值、静默、Ruler、频控与未处理续报、种子「一指标一规则」 |
| **二期** | 已完成 | Webhook / 企微应用 / 电话网关 / 邮件外发；历史复盘 Markdown |
| **三期** | 已完成 | 抑制规则、通知路由（渠道 + 分组）、值班排班 |

后续可选：通知模板编辑器、多 Prometheus 数据源、值班日历 UI、抑制预览试算。

## 数据流

```text
规则模板 + 作用域
    → 编译 compiled_expr（保存时 / 每 5 分钟刷新）
    → Ruler 查 Prometheus（默认 30s，并行最多 8 条）
    → 单阈值：PromQL 含比较符；多档：查指标后 PickAlertTier 取最高档
    → pending → 超过 for → firing（写入 tbl_alerts）
    → 静默？→ handle=silenced（不外发）
    → 抑制？→ 事件 inhibit（落库不外发）
    → 频控 / 续报策略 → 路由渠道 + 值班接收人 → Webhook/企微/电话/邮件
    → 指标回落 → resolved + 可选 resolve 外发
```

分钟级可见：评估周期 30s + `for=1m` 时约 1.5 分钟内可 firing。未配置 `awr_prometheus_base_url` 时 Ruler 空转。

## 一期：规则与评估

### 开放模板（非白名单）

- 手写任意 PromQL + `__SCOPE__` + 作用域；种子 /「选用模板」仅作样例。
- 非 `global` 时模板须含 `__SCOPE__`（≠ 监控面板 `__SEL__`）。

### 多档阈值与通知频控

| 字段 | 说明 |
|------|------|
| `metric_expr`（多档时的 `expr`） | **无比较符**的指标表达式，编译进 `compiled_expr` |
| `tiers[]` | `{op, value, severity, for?}`；命中多档时取级别最高者 |
| `notify_policy.max_fire_per_window` | 计数窗口内最多外发次数，`0`=不限制 |
| `notify_policy.window` | 计数窗口，如 `1h` |
| `notify_policy.repeat_interval` | 未处理续报间隔；空 / `0` 关闭 |
| `notify_policy.repeat_on_handles` | 默认可续报处理态，默认 `["unclaimed"]` |

告警行记录 `last_notified_at` / `notify_count`；事件 `action=notify|inhibit|trigger|resolve`。

**约定**：同一指标只保留 **一条规则 + 多档**，不再拆「过高 / 危急」两条。

### 作用域与 matcher_key

Ruler **每规则只查 1 次** Prom，不做按实例 N+1。

| mode | 含义 |
|------|------|
| `global` | 不注入 matcher（慎用） |
| `instances` | 勾选 KeePlus 实例，由 `matcher_key` 决定标签 |
| `cluster` | 监控域 `idc` + `domain` |
| `tags` | 实例 tags / Prom 标签集合 |

| matcher_key | 行为 |
|-------------|------|
| `auto`（默认） | 全 postgres → `d_b_instance_name`；否则 → `instance`；混选报错 |
| `instance` | 主机 / 多数 exporter |
| `d_b_instance_name` | 腾讯云 PG 实例名（**不**自动附带 `idc`） |
| `domain` / 自定义合法 label | 从实例解析值做 regex 或直匹配 |

大批量：≥50 可折叠同 idc/env；>200 且无法折叠则拒绝超长 regex；勾选上限 1000；单轮 series 上限 500。

可选全局参数 `alert_ruler_interval_sec`（秒，≥10）。

### 主机磁盘 vs 实例存储

| 类型 | 指标 | 建议 matcher_key |
|------|------|------------------|
| 主机磁盘 | `node_filesystem_*` | `instance` |
| PG 实例存储（对齐 Grafana） | `qce_postgres_storagerate_max` | `d_b_instance_name` |

勿把主机磁盘规则绑到 postgres 实例再对照云盘面板。

### 默认种子规则（6 条）

`EnsureDefaultAlertRules` 按固定 ID 幂等插入；用户改过的不覆盖。旧拆分 ID（`host-disk-85/95`、`mysql-conn-80/90`、`pg-storage-80/90`）在仍为 `system` 时会被删除。

| 固定 ID | 名称 | 级别 | 说明 |
|---------|------|------|------|
| `rule-seed-host-disk` | 主机磁盘使用率 | P2/P1 多档 | ≥85% / ≥95% |
| `rule-seed-host-cpu-85` | 主机 CPU | P2 | 单档 |
| `rule-seed-mysql-conn` | MySQL 连接使用率 | P1/P0 多档 | ≥80% / ≥90% |
| `rule-seed-mysql-running` | MySQL 活动连接 | P1 | `threads_running≥64` |
| `rule-seed-pg-conn-80` | PG 连接 | P1 | 单档 |
| `rule-seed-pg-storage` | PG 实例存储 | P1/P0 多档 | ≥80% / ≥90% |

## 二期：外发与复盘

配置入口：**用户权限 → 认证配置 → 通知**。

| 开关 | 行为 |
|------|------|
| `webhook_enabled` + `webhook_on_alert` | POST JSON；若 URL 为企微群机器人则自动改发 markdown |
| `wecom_enabled` + `wecom_on_alert` | 企业微信应用消息（CorpID / AgentID / Secret）；`wecom_to_user` 默认 `@all` |
| `phone_enabled` + `phone_on_alert` | POST JSON 到 `phone_api_url`（severity/title/text 等） |
| `email_on_alert` | SMTP 邮件给值班人、`labels.owner` / `labels.notify` |

实现：`internal/notify/alert.go`、`wecom.go`。负载 `event`：`trigger` / `repeat` / `resolve`。

有通知路由时，渠道以路由为准；否则用上表全局开关。

**复盘**：历史告警页「生成复盘」→ `POST /api/v1/alerts/postmortem`（body: `since` / `until`）返回 Markdown（按级别/服务汇总 + 明细表）。

## 三期：抑制 / 路由 / 值班

| 能力 | 表 | 页面 |
|------|-----|------|
| 抑制 | `tbl_alert_inhibitions` | `/ops/alerts/inhibitions` |
| 路由 | `tbl_alert_routes` | `/ops/alerts/routes` |
| 值班 | `tbl_alert_oncall` | `/ops/alerts/oncall` |

### 抑制

对齐 Alertmanager：源告警 `firing` 且命中 `source_matchers`，目标命中 `target_matchers`，且 `equal_labels`（如 `instance`）取值相同 → 目标**跳过外发**（仍落库，事件 `inhibit`）。

### 通知路由

- `matchers`：空表示默认路由；精确 `k=v`。
- `channels`：`webhook` / `wecom` / `phone` / `email`。
- `group_by`：按标签聚合，通知里带 `group_size`，并回写 `aggregate_count`。
- `priority`：越小越先；`continue=false` 时命中后停止。

### 值班排班

按 `weekday`（0=周日）+ `start_hm`/`end_hm`（支持跨午夜）指定 `usernames`。当前时段值班人作为邮件收件人，并优先覆盖企微 `touser`（用户名需与企微账号一致，否则用全局 `wecom_to_user`）。

## 页面

| 路径 | 说明 |
|------|------|
| `/ops/alerts` | 活跃告警：认领 / 转派 / 处理中 / 恢复 / 误报 / 静默 / 转待办 |
| `/ops/alerts/history` | 历史告警 + 复盘 |
| `/ops/alerts/rules` | 规则 CRUD、多档、频控、作用域、试跑 |
| `/ops/alerts/silences` | 静默规则 |
| `/ops/alerts/routes` | 通知路由 |
| `/ops/alerts/inhibitions` | 抑制规则 |
| `/ops/alerts/oncall` | 值班排班 |

权限 URN 见 [rbac-permissions.md](rbac-permissions.md#告警管理增量)。

## API（均需登录，前缀 `/api/v1`）

| 组 | 方法 |
|----|------|
| 告警 | `GET /alerts`、`/alerts/stats`、`POST /alerts/postmortem`、`GET /alerts/:id`、`GET /alerts/:id/events` |
| 生命周期 | `POST /alerts/:id/claim\|handover\|processing\|resolve\|false-positive\|silence\|todo` |
| 静默 | `GET\|POST /alert-silences`，`POST /alert-silences/:id/cancel` |
| 规则 | `GET\|POST\|PUT\|DELETE /alert-rules`；`GET /alert-rules/templates`；`POST .../test`、`/compile-preview`（body 支持 `metric_expr` / `tiers` / `notify_policy` / `scope`） |
| 抑制 | `GET\|POST\|PUT\|DELETE /alert-inhibitions` |
| 路由 | `GET\|POST\|PUT\|DELETE /alert-routes` |
| 值班 | `GET\|POST\|PUT\|DELETE /alert-oncall` |

写接口：认领类挂 `um.action.alert.claim`；关闭挂 `um.action.alert.resolve`；静默挂 `um.action.alert.silence`；规则/抑制/路由/值班的写删除挂 `um.action.alert.rule.edit` / `rule.delete`。

## 表

| 表 | 要点 |
|----|------|
| `tbl_alert_rules` | `scope_json`、`tiers_json`、`notify_json`、`metric_expr`、`compiled_*` |
| `tbl_alerts` | 含 `last_notified_at`、`notify_count`、`aggregate_count` |
| `tbl_alert_events` | 生命周期与 notify/inhibit |
| `tbl_alert_silences` | 静默 |
| `tbl_alert_inhibitions` | 抑制 |
| `tbl_alert_routes` | 路由（列名 `cont` = continue） |
| `tbl_alert_oncall` | 值班槽 |

多租户：告警规则 / 告警 / 静默按租户隔离，见 [tenancy.md](tenancy.md)。

## 依赖与运维

- Prometheus：`awr_prometheus_base_url`（可选 token）；SQL 洞察设置中配置。
- 通知：认证配置 → 通知 Tab；邮件另需 SMTP。
- 引导版本：修改 migrate/seed 后递增 `SchemaVersion` / `SeedVersion`（见 `internal/store/bootstrap.go`）。
- 菜单由 seed `ensureAlertMenus` 写入；规则由 `EnsureDefaultAlertRules` 幂等保证。
