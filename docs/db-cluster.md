# 集群部署 · MySQL / PostgreSQL

侧栏：**平台运维 → 集群部署** → Hub `/ops/cluster-deploy`。  
工作台：`/ops/cluster-deploy/mysql`、`/ops/cluster-deploy/postgres`。  
菜单 URN：`um.sidebar.ops-cluster-deploy`。API 前缀：`/api/v1/db-deploy/*`。

与 [sr-cluster.md](sr-cluster.md)（StarRocks）、[redis-cluster.md](redis-cluster.md)（Redis/Codis）并列；**入口均为同一 Hub**，工作台布局按引擎差异化。

## 切割

| 能力 | 本模块 | 非本模块 |
|------|--------|----------|
| 自建装包 / 初始化 / 启停 / conf | 是 | — |
| 云托管登记（单入口） | 是（仅登记） | 云厂商开通 / 切主 |
| 配置中心 OSS 预置包 + 版本目录 | 自建 | 不另建 OSS 凭据页 |
| 有从则同步复制闭环验收 | 自建 | — |
| 可选 HA（MySQL·MHA / PG·pg_autofailover 等） | 自建部署时可选接入 | 日常切主仍在 `/ops/ha` |

## 部署形态与拓扑

| deploy_kind | topology | 说明 |
|-------------|----------|------|
| `self` | `standalone` | 自建单机 |
| `self` | `master_replica` | 自建主从（MySQL 半同步 / PG 同步流复制） |
| `cloud` | `cloud_endpoint` | 云托管单入口（含厂商 HA；对用户仍是一个 VIP/域名） |

工作台有**拓扑筛选**（全部 / 自建各拓扑 / 云入口）。新建仅可选自建；登记可选自建或云。云不要求 Agent、不跑 `cluster_deploy`。

- **MySQL 自建**：单机；主从（GTID + 半同步，有从必须 `repl_verify`）
- **PostgreSQL 自建**：单机；主备流复制（`synchronous_standby_names`）
- **云 MySQL/PG**：登记 `endpoint_host` + 云厂商/实例 ID；切主不走本平台 HA
- **自建 HA（MySQL / PostgreSQL）**：仅 `master_replica` 可选；须预登记兼容控制器；装机与复制验收之后自动登记实例、创建 HA 集群。MySQL·MHA 另下发 app.cnf；PostgreSQL·pg_autofailover 做 Monitor 环境检查。失败不回滚装机，状态 `ha_pending`，可「继续接入 HA」（`ha_init`）。云入口禁止平台 HA。

### VIP / 漂移责任矩阵

| 职责 | 落点 | 说明 |
|------|------|------|
| 业务 VIP 元数据 | 部署向导 `ha_vip` → HA 集群 `VIP` | 登记入口地址；钩子注入 `HA_VIP`；**不**自动漂移 |
| MHA 漂移脚本路径 | `ha_options_json` → `master_ip_failover_script` / `master_ip_online_change_script` → app.cnf | 脚本内容由现场维护；平台只下发路径 |
| 切换后钩子 | `vip_hook_script` / `vip_hook_args` → HA 集群 Remark JSON | 计划切换成功后经 Platform Agent 触发 |
| keepalived / LVS 浮动 IP | [接入层代理 · LVS](proxy.md) | **不**并入 MySQL 装机变更单 |

**推荐路径（MySQL · MHA）**：部署勾选 MHA → 填业务 VIP + failover 脚本（可选 vip_hook）→ 复制验收后自动创建 HA 集群并下发 app.cnf → 日常切主走 [高可用服务维护](ha.md)（钩子带 `HA_VIP`）。`ha_precheck` 对「VIP 与脚本皆空 / 仅有 VIP 无脚本」给出 **warning**（不阻断）。

**产品边界（有意不做）**：平台进程内选主；无审批自动切主；代装/托管 etcd；MGR/PXC 组内危险选举命令；VIP/脚本业务内容（仅登记路径与钩子触发）；不把 keepalived 步骤并入 `db-deploy` 装机流水线。

**接入层代理**：Hub 含 HAProxy / Nginx / LVS。**自建部署**经平台 Agent 装发行版包（均含 **keepalived**；须**主备至少 2 节点**；Nginx 可扩至多节点）并下发 conf；VIP 必填且做占用校验。亦可仅登记已有。详见 [proxy.md](proxy.md)。

**与 StarRocks 对照的后续项**：整集群 reinit、版本升级门禁、分层 conf 模板下发；Redis 部署内自动接入 HA。

## 制品

运维将包提前放入配置中心该环境 OSS；`DbRelease` 登记 `object_key` + `sha256`。控制面签发短时 GET，Agent 下载校验解压。对象键建议：

```text
{prefix}db-artifacts/{engine}/{version}/{arch}/{filename}
```

## Agent

三种现场 Agent **进程与端口分开**，不可混填 Base URL：

| Agent | 默认端口 | 共用范围 | 登记入口 | Hub 用途 |
|-------|----------|----------|----------|----------|
| **平台 Agent**（`deploy/backup-agent` → `platform-agent`） | **:8090** | 备份 / Redis RDB / HA 控制器 / **接入层代理** 共用一类 | 配置中心 → 平台 Agent | HAProxy / Nginx / LVS 装发行版包 + keepalived |
| **Node Agent**（`deploy/node-agent`） | **:8091** | **仅 StarRocks**（独立，不与 8090 共用） | SR 工作台 → Node Agent Tab | FE/BE OSS 装机与变更 |
| **DB Agent**（`deploy/db-agent`） | **:8092** | MySQL / PG / Redis / TiDB 等 **db-deploy 引擎共用** | 各引擎工作台 → DB Agent Tab | OSS 制品装机、复制验收、systemd |

`deploy/db-agent` API：`/v1/db/*`（`precheck`、`artifact/install`、`file/*`、`fs/ensure`、`systemd/*`、`exec`、`repl/verify`、`jobs/:id`）。

工作台 **DB Agent** Tab 支持 **SSH 下发**（对齐 StarRocks Node Agent）与手工登记。云托管单入口无需 Agent。下发前：`cd deploy/db-agent && ./build.sh`。

删除顺序：**下线节点 → 删除 Agent → 删除主机**。删除 DB Agent 不受集群节点限制，返回的 `affected_nodes` 列出仍引用它的节点；这些节点在该主机重新下发（或手工登记）Agent 后自动改绑到新 Agent。主机上仍有 Node / DB / Platform Agent 或集群节点时，主机管理删除返回 409 并列出引用。

## 变更单（对齐 StarRocks）

「开始部署」创建 `cluster_deploy` 变更，按拓扑编排：

1. Agent 探活 → 逐节点预检 / 拉包 / 建目录 / 写配置 / systemd / 启动  
2. **MySQL 主从**：GTID + 半同步参数；`repl_setup` + `repl_verify`（`SHOW SLAVE STATUS`）硬门禁  
3. **PG 主备**：`wal_level=replica` + `synchronous_standby_names`；备机 `standby.signal`；`pg_is_in_recovery` 验收  
4. **Redis 主从 / Sentinel**：`replicaof` / sentinel.conf；`INFO replication` / `SENTINEL masters` 验收  
5. **Codis**：dashboard → server → proxy 顺序；联通检查步骤  
6. **可选 HA（MySQL / PostgreSQL）**：`repl_verify` 后按模式执行 `ha_precheck`（含 VIP/脚本 soft warning）→ 登记实例 → 创建 HA 集群（VIP + vip_hook Remark）→（MHA）下发 conf / verify、（pg_autofailover）环境检查 + 拓扑、（Patroni）环境检查 + REST/拓扑、（repmgr）环境检查 + 拓扑、或 Orchestrator/ProxySQL 拓扑探测；全成功 → `ready`；HA 失败 → `ha_pending`，可用 `POST .../ha-init` 重试  
7. **扩容**：ready 集群对新节点点「加入」走 `add_node`（装包+启停；replica 含复制验收）  
8. **下线**：`remove_node` 停服并移除平台登记（保留主机数据目录；禁止下线唯一主）

节点运维：`start` / `stop` / `restart` / `add` / `remove` 走独立变更类型；`ha_init` 计入「部署日志」。工作台「部署日志 / 运行日志」按变更类型过滤。

### 高危操作须工单审批

开始部署、添加节点、删除集群等须先提交 `issue_type=db_deploy` 工单（`POST /api/v1/db-deploy/tickets`，payload 含 `engine`）。**审批流本期已接入**；引擎装机编排按实现进度启用——审批通过后若编排未就绪，工单执行失败并写明原因，**不得**绕过工单直连裸 API。HA 接入重试（`ha-init`）在装机已成功后执行，本期允许工作台直连。

| 操作 | action |
|------|--------|
| 开始部署 | `deploy` |
| 添加节点 | `add_node` |
| 删除节点 | `remove_node` |
| 删除集群 | `delete_cluster` |
| 继续接入 HA | `POST /db-deploy/clusters/:id/ha-init`（非工单） |

### 添加节点：Host/Agent 一致、审批人联动、提交前基础校验

与 StarRocks 工作台规则一致（详见 [sr-cluster.md](sr-cluster.md) 同名小节）：主机从已登记主机（`db_type=host`）下拉选择，DB Agent（:8092）只列出与主机同地址的；审批人按主机加载；「基础校验」通过后才能「提交工单」。

基础校验接口 `POST /api/v1/db-deploy/clusters/:id/nodes/precheck`，请求体为 `{role, host, port, host_instance_id, agent_id, assigned_to}`，校验项：

| 项 | 说明 |
|----|------|
| `cluster` | 集群存在、非云托管、无进行中的变更 |
| `host_instance` | 主机实例存在、类型为 host、有地址，且 Host 属于该主机 |
| `agent_match` | DB Agent 存在且与主机地址一致 |
| `agent_online` | Agent `/healthz` 5 秒内响应 |
| `duplicate` | 集群内无相同 `role@host` 节点 |
| `role_rule` | 角色属于该引擎允许的角色；端口 1–65535；同一主机（跨所有集群）没有节点占用相同端口。端口留空时按角色默认（与编排 `nodePort` 一致，如 sentinel 26379、codis_proxy 19000） |
| `agent:*` / `agent_precheck` | Agent 端预检（`{engine, role, port}`） |
| `approver` | 审批人非空（含 `sync_default_approver` 兜底） |

建单（`action=add_node`）时后端再校验 `agent_match` / `duplicate` / `role_rule`（以及传了 `host_instance_id` 时的 `host_instance`），失败返回 400。

## 前端

Hub 选引擎 → 工作台含：集群列表（**新建集群** / **登记已有集群**）、**变更**、版本目录、**DB Agent**（SSH 下发 + 手工登记）、设置。

| 入口 | 说明 |
|------|------|
| 新建集群 | 状态 `draft`，再点「开始部署」（仅 **可部署** 引擎） |
| 登记已有集群 | `register_existing`，直接 `ready`，不跑装机 |
| 部署日志 / 运行日志 | 变更 Tab 按类型过滤 |

## 引擎能力矩阵

控制面种子：`internal/dbdeploy/registry.go`。Hub：`GET /api/v1/db-deploy/engines`（含与 `ticket_instance_types` 交叉 `warnings`）。动态路由：`/ops/cluster-deploy/:engine`（排除 host）。

| 能力 | 含义 |
|------|------|
| **full** | 自建装机 + 登记 + 云（适用时） |
| **register** | 仅纳管已有/云；禁止 `cluster_deploy` |
| **coming** | Hub 灰卡片（预留） |

| 引擎 | 能力 | 对标模板 |
|------|------|----------|
| starrocks | full | component_chain（srcluster） |
| mysql / postgres / redis·codis | full | repl_pair |
| tidb | full | component_chain（pd→tikv→tidb） |
| cockroachdb | full | component_chain（multi_node join） |
| oceanbase / sqlserver | full | repl_pair（MySQL 协议面 / Linux sqlservr） |
| oracle | full | standalone 自建（ORACLE_HOME 制品）+| cloud |
| clickhouse / mongodb / elasticsearch | full | repl_pair / multi_node 变体 |
| cassandra | full（无云） | component_chain（环 / seed） |
| kafka | full（无云） | component_chain（broker）；Topic 工单仍走 Kafka 运维 |
| haproxy / nginx / lvs | full（无云） | 平台 Agent 装发行版包 + keepalived VIP（非 DB Agent OSS 制品） |

## 新增引擎 checklist（三类模板）

**通用（任何引擎）——对照已落地 MySQL / Redis / StarRocks**

| # | 落点文件 | 抄哪 |
|---|----------|------|
| 1 | [`registry.go`](../internal/dbdeploy/registry.go) | 加 `EngineDescriptor`（拓扑、角色、defaults、capability） |
| 2 | `ticket_instance_types` seed | 同名 `value`；Hub/启动有交叉告警 |
| 3 | [`store/dbdeploy.go`](../internal/store/dbdeploy.go) `validateDbDeployClusterShape` | 允许拓扑 |
| 4 | [`engine.go`](../internal/dbdeploy/engine.go) / [`component.go`](../internal/dbdeploy/component.go) | `validateTopology`、步骤、`component_health` 或 `repl_*` |
| 5 | [`conf.go`](../internal/dbdeploy/conf.go) | softBase / nodePort / renderConf / renderUnit |
| 6 | [`deploy/db-agent`](../deploy/db-agent/main.go) | `repl/verify` 或 component health 分支 |
| 7 | 前端 | 一般不必改；动态路由 + Meta |
| 8 | 本文矩阵 | 补一行 |

### 模板 A · `repl_pair`（对标 MySQL/PG/Redis；ClickHouse 主从变体）

落点：`validateTopology` 主从角色 → `buildDeploySteps` 装机后 `repl_setup`/`repl_verify`（或等价 health）→ `conf.go` → Agent 验收。

### 模板 B · `component_chain`（对标 StarRocks / TiDB / CRDB / Cassandra / ES / Kafka）

落点：角色齐全校验 → `orderNodesForDeploy` → 逐步装启 → `component_health` → conf/unit 按角色。

### 模板 C · 商业库自建（OceanBase / Oracle / SQL Server）

落点：`capability=full`、`AllowDeploy=true`。

| 引擎 | 自建拓扑 | 说明 |
|------|----------|------|
| oceanbase | standalone / master_replica | env + observer/`start.sh`；主从走 MySQL 协议 CHANGE MASTER |
| oracle | standalone | `oracle.env` + `dbstart` / 制品 `dbstart_wrapper.sh` |
| sqlserver | standalone / master_replica | `mssql.conf` + `sqlservr`；主从细节由制品脚本 |

云托管仍用 `cloud_endpoint`。制品与 License 由客户提供；平台负责下发、写配置、systemd、探活。

**不做**：另起控制面表；热插拔插件框架（≥3 个分布式引擎成型后再议）。

