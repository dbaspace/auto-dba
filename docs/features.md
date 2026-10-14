# 功能说明

<p>
  <img src="assets/keeplus-lockup.svg" alt="KeePlus" width="220" />
</p>

**KeePlus**（工程仓库 / 进程名 `auto-dba`）是面向 DBA 与开发的运维自助工作台。业务对象（实例、库、账号、审批人）登记在平台内，工单、查询、同步、迁移、闪回、归档、集群部署与告警都围绕这些登记对象执行。页面权限按菜单树授予，写操作另有独立的操作权限；业务数据按**多租户**行级隔离。

侧栏按下面的目录组织。目录本身不可进入，子项才是页面。模块关系总览见 [architecture.html](architecture.html)。集群部署（StarRocks 等工作台）方案见 [sr-cluster.md](sr-cluster.md)。

**部署与配置**：元数据为 PostgreSQL。本地可只用 `config/config.yaml`；生产推荐敏感项进 **Nacos**（本地仅引导），见 [nacos-config.md](nacos-config.md)。**打包部署**（一体 / 仅后端 / 仅前端 / Docker）见 [deploy.md](deploy.md)（勿在 CentOS 7 上装 Node 构建）。**多租户**模型、划拨、跨租授权与 ACL 见 [tenancy.md](tenancy.md)。日常怎么用见 [ops-manual.md](ops-manual.md)。

## 角色

| 角色 | 说明 |
|------|------|
| 管理员 | 全部功能，含用户、角色、菜单等权限治理 |
| 运维 | 业务配置与运维操作，不含用户 / 角色 / 菜单治理 |
| 开发者 | 与运维的业务权限相当，不含权限治理。LDAP / SSO 首次登录自动创建的本地用户默认此角色 |
| 只读 | 仅可查看已授权页面，不能新建、编辑或删除 |

本地种子账号为 `admin` / `admin`，首次登录后应修改密码。

## 登录与账号

- **本地登录**：用户名密码，带验证码。连续失败会锁定账号。
- **LDAP**：按目录校验登录名，把邮箱写入用户管理，显示名取中文名（与登录名相同的属性会跳过）。
- **登录页记忆**：按认证方式（平台 / LDAP / SSO）分别记住上次用户名；LDAP 不再预填平台 `admin`。密码不落本地。
- **SSO**：支持授权码与密码模式，登录后换取本系统 Token。详见下文「SSO」。
- **选租户**：用户可属多个租户；登录后若有多个有效成员则进入 `/select-tenant`，JWT 写入当前 `tid`。顶栏可切换租户（业务租户成员）。
- **LDAP / SSO 首次登录**：自动创建本地用户（默认角色 developer），且仅在尚无任何有效租户成员时加入**默认租户**；之后由管理员加入业务租户（普通用户加入非 default 后会离开 default；平台管理员可同时保留默认租户）。详见 [tenancy.md](tenancy.md#成员归属)。
- **忘记密码**：通过已配置的邮件发送重置链接。
- **空闲退出**：登录后按全局空闲时长自动登出；时长为 0 时关闭。
- **水印**：可按用户开启页面水印。

认证、邮件和通知开关都在「用户权限 → 认证配置」。通知 Tab 可分别开关：工单创建（审批人）、执行失败（提交人）、审批后待执行（提交人）、待执行超时告警（提交人，默认 2 小时、同一工单只发一次）、备份失败；以及告警外发（Webhook / 企微应用 / 电话网关 / 邮件，各有独立「用于告警」开关）。邮件类需 SMTP 就绪且对应用户已绑定邮箱。告警渠道与路由说明见 [alerts.md](alerts.md)。

### SSO

配置页：用户权限 → 认证配置 → **SSO**。开关与 Client Secret 等保存在元库全局参数 `sso_config`（Client Secret 用当前 `app.jwt_secret` 加密）。启用并保存后，登录页「认证方式」出现 SSO。

| 授权模式 | 登录方式 | 说明 |
|----------|----------|------|
| 授权码（默认） | 登录页选 SSO 后跳转 IdP | 须填写与 IdP 白名单**逐字相同**的回调地址；跳转 Authorize **不依赖** Client Secret，换票才需要 |
| 密码 | 登录页选 SSO 后填账号密码 | 需 IdP 支持 password grant |

**回调地址（授权码）**

- 前端回调示例：`http://127.0.0.1:5174/auth/callback`（本地 Vite）或生产前端 Origin + `/auth/callback`。
- 也可登记后端回调：`http://<api-host>/api/v1/auth/sso/callback`。
- IdP 通常不把 `localhost` 与 `127.0.0.1` 视为同一地址，本地开发请与打开页面的 Host 保持一致。

**与 `jwt_secret` 的关系**

Client Secret（及 LDAP Bind 密码）用字段加密密钥落库（优先 `app.crypto_secret`，未配置时回退 `jwt_secret`；密文为 `enc:v2`）。通过 Nacos **更换 `crypto_secret` 且未保留 `crypto_secret_previous`**（或旧部署只换了 `jwt_secret` 且曾用其加密）后，旧密文无法解密：登录页仍可能显示 SSO（`enabled` 仍为 true），但换票会提示需重新保存 Secret。处理：到认证配置 → SSO **重新填写 Client Secret 并保存**（LDAP 同理重存 Bind 密码）；或轮换时先填 `crypto_secret_previous`。详见 [Nacos 配置](nacos-config.md)。

**排障**

| 现象 | 常见原因 |
|------|----------|
| 登录页没有 SSO | 未启用、未点保存，或前端代理与后端连的不是同一元库 |
| 跳转 Authorize 失败 / 提示未启用 | 库中 `enabled` 仍为 false（改开关后未保存） |
| 能跳 IdP，换票提示 Secret 解密失败或需重新保存 | `jwt_secret` 已变更，或从未正确保存过 Client Secret |
| 回调报 redirect_uri 不一致 | 回调地址与 IdP 白名单、当前页面 Origin（含 localhost vs 127.0.0.1）不一致 |

## 仪表盘

路径 `/ops/dashboard`。汇总工单数量与状态、按类型和机房分布，并提供工单草稿入口与近期查询审计摘要。可从这里跳到各类工单申请页。

## 数据查询

在已登记实例上做只读探查，语句写入查询审计。

| 页面 | 路径 | 说明 |
|------|------|------|
| 数据库 | `/ops/query` | 选择环境与库执行 SQL，支持收藏语句、取消执行、同名库切换环境 |
| Kafka | `/ops/kafka-query` | 查看 Topic 元数据并抽样消息 |
| Redis | `/ops/redis-query` | 侧栏按 **环境 → 域名 → 逻辑库** 选择连接（优先域名表）；执行允许的只读命令 |

## 工单

申请类工单提交后进入审批；审批人来自已登记库。未登记的库或非审批人不能提交。工单列表可按状态跟踪，详情页查看审批与执行结果。提交者可在待审批时[更换审核人](ticket-assignee-transfer.md)；超级审核白名单见全局参数 `issue_global_approver_whitelist`。

**数据源按实例域名选择**：DDL / DML / 建库 / 权限申请的「数据源」选项来自实例已登记的 `domains[]`（一实例多域名展开为多条，标签为域名而非 host:port）。选定域名后，逻辑库列表只保留绑定了该域名的库；提交写入工单 `domain`，执行时以该域名作为连接 Host（与库-域名绑定一致）。

| 页面 | 路径 | 说明 |
|------|------|------|
| 工单列表 | `/ops/tickets` | 全部工单的查询、审批与执行记录 |
| DDL 工单 | `/ops/ddl` | 结构变更。提交前按审核规则做 SQL 检查，可看执行影响；可多选域名级数据源 |
| DML 工单 | `/ops/dml` | 数据变更，同样走审核与审批。PostgreSQL DML 由平台包事务执行，脚本中的 `BEGIN`/`COMMIT`/`ROLLBACK` 会被忽略（避免与外层事务双重提交） |
| DQL 工单 | `/ops/dql` | 数据导出。左侧填环境、库、审批人和事由，右侧写查询 SQL |
| Kafka 工单 | `/ops/kafka` | 创建或调整 Topic（分区、副本、保留策略等） |
| Redis 工单 | `/ops/redis` | 按 Redis 命令规则提交变更，规则在审核规则中维护 |
| 权限申请 | `/ops/dcl` | 为数据库账号申请权限，并选择目标库 |
| 新增数据库 | `/ops/createdb` | 在已登记实例上申请建库 |

申请页支持草稿。DDL / DML 在执行前可生成影响说明。DDL / DML / 建库在审批通过或手动执行后先置「执行中」并立即返回，后台脱离 HTTP 超时执行；详情页轮询至成功或失败。大批量结果自动收敛（按库汇总计数、失败全留、成功抽样、清空冗长 success_sqls），防止结果 JSON 撑爆内存或落库失败。不抬高全局 HTTP WriteTimeout。执行失败、审批后仍待执行、以及长时间停在待执行，可按通知配置邮件告知提交人。

## 同步与迁移

| 页面 | 路径 | 说明 |
|------|------|------|
| 同步列表 | `/ops/slots` | 采集并查看 PostgreSQL 复制槽。未连接真实库时写入本地占位记录 |
| CDC 同步任务 | `/ops/cdc` | 创建、启动、停止同步任务，查看链路、表映射、加表与运行日志；Debezium Connector 缺失时可[重新初始化](cdc-reinit.md) |
| 数据迁移 | `/ops/migration` | 库表或整库迁移，支持对象选择、预检、执行与校验 |
| 表结构对比 | `/ops/migration/compare` | 同族实例异步对比结构，生成只增不删的修复 SQL |

**CDC** 的链路是 PostgreSQL 逻辑复制 → Debezium / Kafka Connect → Kafka → StarRocks。一个任务内维护源表与目标表的绑定；分库分表按绑定串行处理，提交前只对代表库做表存在性预检。外部地址未配置时任务可以先落库，执行时返回「未配置」，不会中断进程。

**数据迁移** 可在「迁移设置」里配置审批与全局性能参数；对象存储在「配置中心 → 对象存储」维护，创建任务选择 OSS 中转时直接引用。

**表结构对比** 只支持同族互比：PostgreSQL 族之间，或 MySQL / TiDB / OceanBase 之间。修复 SQL 方向是源端到目标端，不生成删除语句。详见 [表结构对比](migration-schema-compare.md)。

## 数据安全

### 数据备份

面向已登记 **PostgreSQL / MySQL** 实例的逻辑全量备份：策略、手动/定时执行、本地或 S3 存储、运行观测；生产推荐 Backup Agent。与闪回、归档「备份到目标」互补。设计见 [数据库备份管理系统](backup.md)。

| 页面 | 路径 | 说明 |
|------|------|------|
| 备份策略 | `/ops/backup/policies` | CRUD、逻辑/物理方式、启用/停用、立即执行 |
| 执行记录 | `/ops/backup/runs` | 状态、大小、校验与日志（含物理链 parent） |
| 备份存储 | `/ops/backup/settings` | 本地/S3 存储池 |
| 平台 Agent | `/config/agents` | 配置中心登记 / SSH 下发；归属 backup/rdb/ha，业务按归属引用 |

物理备份（`physical_full` / `physical_incr`）强制 Agent：`pg_basebackup` / `xtrabackup`。PITR 恢复工单见后续迭代。

### 数据闪回

面向 PostgreSQL，用 WAL 与堆页死行把误删或误改的数据找回来。默认不修改 `REPLICA IDENTITY`；普通表在 VACUUM 前靠全页写和堆页补齐非主键列。

| 页面 | 路径 | 说明 |
|------|------|------|
| 仪表盘 | `/ops/flashback/dashboard` | 闪回概览与快捷入口 |
| 闪回任务 | `/ops/flashback/tasks` | 按库表和时间范围发起闪回 |
| 历史记录 | `/ops/flashback/history` | 已执行闪回的结果与明细 |
| 多云密钥 | `/ops/flashback/cloud` | 闪回使用的云厂商访问密钥 |

### 归档管理

把在线表中的历史数据按策略迁出，并保留执行与告警记录。

| 页面 | 路径 | 说明 |
|------|------|------|
| 控制台 | `/ops/archive/dashboard` | 任务与执行概况 |
| 归档任务 | `/ops/archive/apply` | 提交归档申请 |
| 任务管理 | `/ops/archive/tasks` | 启停、编辑已有任务 |
| 执行记录 | `/ops/archive/runs` | 每次运行的结果 |
| 监控告警 | `/ops/archive/monitor` | 运行异常与告警 |
| 系统设置 | `/ops/archive/settings` | 归档参数与通知 |
| 敏感数据脱敏 | `/ops/archive/masking` | 归档结果中的脱敏规则 |
| 归档报表 | `/ops/archive/report` | 归档量与执行统计 |

## 诊断

| 页面 | 路径 | 说明 |
|------|------|------|
| SQL 洞察 | `/ops/sql-insight` | 六 Tab：SQL洞察 / 洞察分析 / 可视汇总 / AWR报告 / 表空间分析 / 智能分析；壳上统一「设置」入口（`/config/sql-insight`）。写操作权限：活动快照采集 / AWR 生成 / 表空间采集 / 智能分析（见 rbac-permissions） |
| 会话管理 | `/ops/sessions` | 查看会话与锁，可定时刷新，并结束异常会话 |

## 告警管理

平台自建 Ruler，查询全局 Prometheus，管理规则 / 活跃与历史告警 / 静默；支持多档阈值、频控续报、Webhook·企微·电话·邮件外发、抑制、通知路由与值班排班。设计与 API 见 [告警管理](alerts.md)。AIOps 见 [AIOps](aiops.md)。

| 页面 | 路径 | 说明 |
|------|------|------|
| 活跃告警 | `/ops/alerts` | 认领、转派、处理中、恢复、误报、静默、转待办；详情含诊断摘要 / Runbook / 助手 |
| AIOps 知识库 | `/ops/aiops/knowledge` | 内置与库内案例（PostgreSQL/MySQL/Redis/StarRocks）、alertname 试算、DBA Copilot |
| AIOps 关联 | `/ops/aiops/incidents` | 按实例时间窗聚合活跃告警 |
| 历史告警 | `/ops/alerts/history` | 已恢复列表与复盘 Markdown |
| 规则管理 | `/ops/alerts/rules` | PromQL 模板、作用域、多档、频控、试跑 |
| 静默管理 | `/ops/alerts/silences` | 按 labels 静默 |
| 通知路由 | `/ops/alerts/routes` | 按 matchers 选渠道与分组 |
| 抑制规则 | `/ops/alerts/inhibitions` | 源告警压制目标外发 |
| 值班排班 | `/ops/alerts/oncall` | 按星期与时段指定接收人 |

依赖：`awr_prometheus_base_url`（SQL 洞察设置）；外发在认证配置 → 通知 Tab。

## 平台运维

| 页面 | 路径 | 说明 |
|------|------|------|
| 多语模版 | `/ops/multilang` | 多语言模版任务的创建与列表 |
| 海豚管理 | `/ops/dolphin` | 检查外部调度控制台是否可达，可达时打开控制台 |
| SR 控制台 | `/ops/sr` | **仅**检查 StarRocks FE 管理端是否可达并打开控制台 |
| 集群部署 | `/ops/cluster-deploy` | Hub 由 EngineRegistry 驱动：DB/OLAP 自建装机与登记；**接入层代理** HAProxy/Nginx/LVS（发行版包 + **Keepalived VIP**，VIP 占用校验）。详见 [db-cluster.md](db-cluster.md)、[proxy.md](proxy.md)。 |
| 主机终端 | `/ops/terminal` | 对类型为主机的实例打开 Web SSH。密钥布局、登记与排障见 [主机终端](terminal.md)。 |
| 高可用服务维护 | `/ops/ha` | MySQL/PG/Redis HA：观测、维护、计划/强制切换；MHA/Orchestrator/ProxySQL；PostgreSQL·pg_autofailover / Patroni / repmgr（代装、Keeper 启停、VIP 钩子）。详见 [高可用服务维护](ha.md)。 |

海豚与 SR 控制台未配置外部地址时，页面只提示未配置，不提供入口。高可用未配置匹配控制器时不自动 failover。**集群部署**负责新建/登记后的扩缩/升级编排，**不是**高可用切换；各引擎工作台布局不同，见 `docs/sr-cluster.md` / `docs/db-cluster.md` / `docs/redis-cluster.md`。

## 多租户

共享部署 + 元库行级 `tenant_id`。默认严格隔离；跨租仅通过**成员加入**、**资源授权（grant，只读）**或平台管理员**审计 impersonate**。设计、API 与防串租清单见 [tenancy.md](tenancy.md)。

| 能力 | 说明 |
|------|------|
| 租户管理 | `/config/tenants`：创建/编辑/删除租户（删除须无成员且无绑定资源）、成员与角色、跨租授权、审计日志；平台管理员可「进入租户审计」。顶栏显示租户名称；平台管理员可同时保留默认租户成员 |
| 业务权限 | 侧栏与业务 URN 按**当前租户成员** `role_ids`；平台 URN（用户/菜单/LDAP 等）走全局角色 |
| 可见范围 | 实例/库「全局」= 本租成员可见；「私有」= 仅可访问用户或绑定角色命中（候选项与写入均须本租成员）。异租用户不能靠填 ACL 跨租访问 |
| 划拨 | 审计会话下可将实例/已登记库/**域名**/部署集群/代理划到指定租户；**库与域名可单独归属**（不迁实例），均支持批量指定租户 |
| 跨租授权 | 资源方租户选出实例/库/域名/集群，授权给目标租户只读；弹窗按类型拉 `grant-resources` |

删除用户时会清理其租户成员与按用户授权，避免幽灵成员行。

## 用户权限

菜单即权限目录。角色勾选页面后才能进入；页面上的新建、编辑、删除等按钮对应单独的操作权限，后端接口按同一 URN 校验。登记方式见 [权限登记指南](rbac-permissions.md)。

| 页面 | 路径 | 说明 |
|------|------|------|
| 租户管理 | `/config/tenants` | 租户 CRUD（空租户可删）、成员、跨租授权、审计进入；见上文「多租户」 |
| 用户管理 | `/config/users` | 本地用户、角色、启用与禁用（平台级） |
| 角色授权 | `/config/roles` | 为角色勾选页面和操作。勾选操作会带上所属页面；取消页面会取消其下操作 |
| 菜单管理 | `/config/menus` | 维护侧栏页面与操作权限节点 |
| 登录日志 | `/config/login-logs` | 登录成功与失败记录 |
| 认证配置 | `/config/ldap` | LDAP、SSO、邮件、通知四个配置页 |
| 查询审计 | `/config/query-audits` | 数据库、Kafka、Redis 查询语句的审计检索 |

## CMDB

DBA 域配置管理门面：统一目录（含 SR/部署/代理/账号/CDC）、关系拓扑（depth≤3）、影响分析、健康徽章、快照漂移、业务标签聚合与可选 Webhook。实例/库等仍在「数据库配置 / 配置中心」维护，CMDB 不改写工单上的 `instance_id`。详见 [CMDB 模块](cmdb.md)、[操作手册 · CMDB](ops-manual.md#41-cmdb-日常操作)。

| 页面 | 路径 | 说明 |
|------|------|------|
| 资产目录 | `/cmdb/assets` | 按类型浏览；健康徽章、业务标签、影响分析与快照 |
| 关系拓扑 | `/cmdb/topology` | 推导/手工关系；深度 1–3；影响分析 |
| 标签管理 | `/cmdb/tags` | 统一标签字典（原 `/config/tags` 跳转至此） |
| 发现登记 | `/cmdb/discover` | 监控/集群/Agent 候选、Live 库登记、质量概览与漂移 |
| 导入导出 | `/cmdb/import-export` | 实例 CSV/JSON |
| 变更审计 | `/cmdb/audit` | 实例/库等变更 before/after |
| 同步任务 | `/cmdb/sync` | Prometheus / 云标注 / HTTP 企业 CMDB 适配器（默认仅候选） |

## 数据库配置

工单、查询和同步都依赖这里的登记数据。实例未登记、库未登记或当前用户不是该库审批人时，提交会返回明确错误。资源带 `tenant_id`；可见范围与审批人受多租户 ACL 约束（见 [tenancy.md](tenancy.md)）。

| 页面 | 路径 | 说明 |
|------|------|------|
| 实例管理 | `/config/instances?db_type=…` | 登记 PostgreSQL、MySQL 族、Kafka、Redis、主机等连接；按类型各开 Tab；列表支持列排序与底部分页；域名/标签列表折叠；域名可多行粘贴并去重；可见范围（全局/私有）；审计下「指定租户」（可选目标） |
| 已登记库 | `/config/databases` | 登记业务库、审批人与可见范围；审计下可单条或**批量指定归属租户**（不迁实例） |
| 域名管理 | `/config/domains` | 全引擎统一域名资源；按类型一键同步（`UNIQUE(domain,db_type)` 隔离）；新域名默认归属默认租户；审计下单条/批量指定租户；可授权跨租只读。工单（DDL/DML/建库/权限申请）「数据源」下拉按**本租户可见域名**列出，不再按实例租户展开 |
| 账号管理 | `/config/accounts` | 登记供权限申请和执行使用的数据库账号（随库租户） |
| 审核规则 | `/config/review-rules` | DDL / DML 的 SQL 审核规则，以及 Redis 命令规则。PostgreSQL 时间字段等多值规则见 [review-rules.md](review-rules.md) |

## 配置中心

| 页面 | 路径 | 说明 |
|------|------|------|
| 全局参数 | `/config/args` | 审批开关、默认审批人、SQL 约束、登录空闲时长、水印等 |
| 区域管理 | `/config/regions` | 机房或区域，并可绑定环境 |
| 环境管理 | `/config/environments` | 生产、测试等环境 |
| 连接器 | `/config/connectors` | CDC、迁移等外部组件的连接信息 |
| 平台 Agent | `/config/agents` | 平台级执行面登记 / 下发；备份、RDB、高可用按归属引用 |
| 对象存储 | `/config/oss` | 按环境 + 云产品维护；分片上传/断点续传/后台 SHA-256 校验。详见 [oss-upload.md](oss-upload.md) |

## 使用上的共同约束

- 目标库必须先在「实例管理 / 已登记库」登记，并指定审批人。
- 页面能否打开、按钮能否点击，由**当前租户**成员角色上的菜单和操作权限决定（平台治理页除外）。
- 业务请求须带活跃租户（JWT `tid` / `X-Tenant-Id`）；无租户时前端进入选租户页。
- CDC、迁移、海豚、StarRocks 的外部地址未配置时，相关任务可以保存，真正执行或打开控制台时返回未配置，而不是进程异常退出。
- 元数据存在本系统的 PostgreSQL 中，启动时按 `schema_version` / `seed_version` 引导建表与种子；敏感连接信息可放 Nacos，见 [nacos-config.md](nacos-config.md)。
