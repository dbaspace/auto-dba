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

**与 StarRocks 对照的后续项**：整集群 reinit、版本升级门禁、已运行实例的参数在线下发（含回滚 / 修订记录）；Redis 部署内自动接入 HA。

## 制品

运维在 **版本目录** Tab 登记版本，包放在配置中心该环境 OSS（设置 Tab 绑定 OSS 与 prefix，未填 prefix 时默认 `db-artifacts/{engine}/`）。对象键约定：

```text
{prefix}{version}/{arch}/{filename}
```

- **版本号按引擎校验**：MySQL `主.次.修订`（5.7 / 8.0 / 8.4 LTS / 9.x Innovation，通道 LTS / Innovation / GA）；PostgreSQL `主版本[.小版本]`，**13 及以上**（内置模板使用 `wal_keep_size`）。列表展示大版本（MySQL `8.0`，PG `16`）。
- **登记时 object_key 可留空**：按约定自动生成；只填文件名也会补全目录。
- **取包三种方式**：
  - 页面 **上传**：边传边算 sha256，写入 OSS 并回填。
  - **从 OSS 同步**：列出版本目录，取同名或最大的包。sha256 依次取同目录 `.sha256` 旁注、`SHA256SUMS` / `sha256sums.txt`、对象元数据；都没有时，≤32MB 当场计算，更大的包后台计算，状态显示「计算中」。
  - **行内填写 sha256**。
- **就绪状态**：object_key 指向具体文件且 sha256 为 64 位十六进制即为「就绪」。**部署 / 扩容只允许已启用且就绪的版本**（签发下载地址时校验），新建集群的版本下拉也只列就绪版本。
- **安装解包**：MySQL / PG 官方 tarball 带一层顶级目录，控制面在 `artifact/install` 传 `strip_components: 1`。DB Agent 仅在包内恰好只有一个顶级目录时才剥离，否则原样解压。旧版 Agent 忽略该字段，需重新下发 DB Agent 才生效。

## 参数模板（MySQL / PostgreSQL）

部署 / 扩容写配置（`put_conf_*` 步骤）时，MySQL 写 `{software_base}/my.cnf`，PG 写 `{data_root}/postgresql.conf`。生成步骤：

1. **选模板**（按集群版本的大版本）：
   - 平台模板按以下顺序查找：精确大版本 → 内置对应大版本（MySQL 8.1–8.3 → 8.0，8.4 / 9.x → 8.4）→ `*` 通用。
   - 都没有时用内置模板：MySQL 5.7 / 8.0 / 8.4，PG 通用（13+）。
2. **渲染变量**：Go `text/template`，未知变量直接报错。可用 `{{.BaseDir}} {{.DataDir}} {{.LogDir}} {{.Port}} {{.Host}} {{.ServerID}} {{.Role}} {{.Topology}} {{.Version}} {{.Major}}`。
3. **叠加集群覆盖**：每行 `key = value`，同名参数替换模板值，否则追加。MySQL 只作用于 `[mysqld]`，`-` 与 `_` 等价；不支持段头、模板变量。
4. **追加平台托管项**：模板和覆盖里都不允许填写，平台按拓扑与角色生成：
   - **MySQL**：`basedir / datadir / port / socket / server_id`；只读从库加 `read_only / super_read_only`。主从拓扑会加载半同步插件 `plugin-load-add`，参数名随版本切换：8.4+ 用 `rpl_semi_sync_source_* / replica_*`，更早版本用 `master_* / slave_*`。
   - **PG**：`port / data_directory`；主从拓扑主库加 `synchronous_standby_names`，从库加 `primary_conninfo / hot_standby`。

入口：

- **设置 Tab →「参数模板」**：按大版本分页签，可编辑并保存为平台模板、恢复内置、复制成新大版本模板。保存时校验托管键，并用示例变量试渲染。
- **集群详情 →「参数」**：左侧编辑集群覆盖，右侧预览各节点最终配置及模板来源（未保存的覆盖也可预览）。

范围：只对**后续部署 / 扩容**生效；已运行实例的在线下发后续提供。

接口：`GET|PUT /db-deploy/conf-templates`、`POST /db-deploy/conf-templates/reset`、`GET|PUT /db-deploy/clusters/:id/conf`、`GET|POST /db-deploy/clusters/:id/conf/preview`。表：`tbl_db_deploy_conf_templates`（`engine, major` 唯一）、`tbl_db_deploy_cluster_confs`（`cluster_id` 主键）。

## Agent

三种现场 Agent **进程与端口分开**，不可混填 Base URL：

| Agent | 默认端口 | 共用范围 | 登记入口 | Hub 用途 |
|-------|----------|----------|----------|----------|
| **平台 Agent**（`deploy/backup-agent` → `platform-agent`） | **:8090** | 备份 / Redis RDB / HA 控制器 / **接入层代理** 共用一类 | 配置中心 → 平台 Agent | HAProxy / Nginx / LVS 装发行版包 + keepalived |
| **Node Agent**（`deploy/node-agent`） | **:8091** | **仅 StarRocks**（独立，不与 8090 共用） | SR 工作台 → Node Agent Tab | FE/BE OSS 装机与变更 |
| **DB Agent**（`deploy/db-agent`） | **:8092** | MySQL / PG / Redis / TiDB 等 **db-deploy 引擎共用** | 各引擎工作台 → DB Agent Tab | OSS 制品装机、复制验收、systemd |

`deploy/db-agent` API：`/v1/db/*`（`precheck`、`artifact/install`、`file/*`、`fs/ensure`、`systemd/*`、`exec`、`repl/verify`、`jobs/:id`）。

工作台 **DB Agent** Tab 支持 **SSH 下发**（对齐 StarRocks Node Agent）与手工登记。云托管单入口无需 Agent。一体/后端发布包与 Docker 镜像已含 `deploy/db-agent/bin/db-agent`；从源码跑控制面时再执行 `cd deploy/db-agent && ./build.sh`。

**对已有集群安全**：SSH 下发 / 重新下发 **仅**安装或重启 Agent 自身（默认 `:8092`），不启停 MySQL/PG/Redis 等已运行服务、不改业务 systemd/二进制/数据盘；禁止将业务端口配为 Agent 监听；`fuser -k` 仅在约定端口带 8090–8199。

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

节点运维：`start` / `stop` / `restart` / `add` / `remove` 走独立变更类型。「变更」页按类型分四个子 Tab：初始化日志（`cluster_deploy`、`ha_init`）、运行日志（`start_node` / `stop_node` / `restart_node`）、升级日志（暂无此类变更）、扩缩容日志（`add_node` / `remove_node`），标签带条数；打开任一变更时子 Tab 自动切到其分类。集群卡片上不再有日志按钮。

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
| `approver` | 审批人非空（含 `sync_default_approver` 兜底）；删除/部署工单弹窗按节点主机加载审批人并预填第一项，无主机时回退全局默认 |

建单（`action=add_node`）时后端再校验 `agent_match` / `duplicate` / `role_rule`（以及传了 `host_instance_id` 时的 `host_instance`），失败返回 400。

## 前端

Hub 选引擎 → 工作台含：集群列表（**新建集群** / **登记已有集群**）、**变更**、版本目录、**DB Agent**（SSH 下发 + 手工登记）、设置。

| 入口 | 说明 |
|------|------|
| 新建集群 | 状态 `draft`，再点「开始部署」（仅 **可部署** 引擎） |
| 登记已有集群 | `register_existing`，直接 `ready`，不跑装机；填节点 Host 后自动匹配/登记 DB Agent（`:8092`） |
| 初始化 / 运行 / 升级 / 扩缩容日志 | 变更 Tab 内分类子 Tab |

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

