# 打包与部署

KeePlus（进程 `auto-dba`）支持多种打包方式。**构建在有 Node 18+ / Go 的机器上完成**；CentOS 7 等运行机只落产物，**不要**在运行机上 `pnpm build`。

开发态（Vite `:5174` + API `:8800`）见 [README](../README.md) 与 [nacos-config.md](nacos-config.md)；本节只讲**生产/预发部署**。

## 端口说明（易混淆）

| 场景 | 页面 | API |
|------|------|-----|
| 本机开发 | Vite **5174**（`pnpm -C frontend dev`） | **8800**，Vite 代理 `/api` |
| 一体包 / Docker（`serve_static: true`） | **与 API 同端口，默认 8800** | 8800 |
| Nginx + 后端分离 | Nginx 80/443 | 后端 8800，`/api` 反代 |

生产一体部署**没有** 5174，这是预期行为。

## 打包命令一览

在仓库根目录执行 [`scripts/pack_release.sh`](../scripts/pack_release.sh)：

| 模式 | 命令 | 产物 | 适用 |
|------|------|------|------|
| **一体**（默认） | `./scripts/pack_release.sh` 或 `… all` | `dist/auto-dba-release-linux-amd64-*.tgz` | CentOS 手工部署，一进程托管页面+API；**含** Node/DB/Platform Agent |
| **仅后端** | `./scripts/pack_release.sh backend` | `dist/auto-dba-backend-linux-amd64-*.tgz` | 前后端分离时的 API；**含**三类 Agent（SSH 下发用） |
| **仅前端** | `./scripts/pack_release.sh frontend` | `dist/auto-dba-frontend-*.tgz` | Nginx/CDN 静态资源；**不含** Agent |
| **Docker** | `./scripts/pack_release.sh docker` | 镜像 `auto-dba:<时间戳>`（可用 `IMAGE_TAG=` 覆盖） | 容器环境；镜像内 **含** Agent |

一体包与后端包解压后工作目录须为**包根**（存在 `deploy/node-agent/bin/node-agent`、`deploy/sql-agent/bin/sql-agent` 等），集群部署和 SQL 执行器 SSH 下发无需再本机构建。

可选环境变量：`GOOS` `GOARCH`（默认 `linux`/`amd64`）、`STAMP`（默认时间戳；发版用 `STAMP=v1.12.156`）、`IMAGE_TAG`、`SKIP_PNPM_INSTALL=1`（已有 `node_modules` 时）。

```bash
STAMP=v1.12.156 ./scripts/pack_release.sh all
STAMP=v1.12.156 ./scripts/pack_release.sh backend
STAMP=v1.12.156 ./scripts/pack_release.sh frontend
IMAGE_TAG=auto-dba:v1.12.156 ./scripts/pack_release.sh docker
```

## 发布到 GitHub Release（固定流程）

用 [`scripts/publish_github_release.sh`](../scripts/publish_github_release.sh)，**不要**依赖交互式 `gh auth login`（浏览器 OAuth 易超时）。

凭据来源与 `git push https://github.com/...` 相同：从本机 **git credential / Keychain** 取出 token，临时注入 `GH_TOKEN` 再调 `gh`（与 v1.11.0 / v1.12.0 发版成功路径一致）。

```bash
# 已 push 的提交 + 写好 docs/changelog-vX.Y.Z.md 后：

# 推荐：打包 + 打 tag + 创建 Release（一体包）
PACK=1 ./scripts/publish_github_release.sh v1.12.156

# 已有 dist/*-v1.12.156.tgz 时只发版
./scripts/publish_github_release.sh v1.12.156

# 发版前顺带删若干旧 Release（不删受仓库 Rules 保护的 tag）
DELETE_RELEASES='v2.3.6 v2.3.7' PACK=1 ./scripts/publish_github_release.sh v1.12.156
```

前提：本机能 `git push origin HEAD`；已安装 `gh`。若 Keychain 无凭据，先手动 push 一次或导出 `GH_TOKEN=...`。

### 已发布版本（摘要）

| 版本 | 要点 |
|------|------|
| [v1.12.156](changelog-v1.12.156.md) | **当前推荐**。定时任务按分组展示，执行历史可以翻页 |
| [v1.12.155](changelog-v1.12.155.md) | PostgreSQL DDL 禁止没有 SET LOCAL 的显式事务 |
| [v1.12.154](changelog-v1.12.154.md) | 归档按执行记录统计，脱敏和全局参数收口 |
| [v1.12.153](changelog-v1.12.153.md) | 归档补齐删除、脚本和告警，闲置超时在线生效 |
| [v1.12.152](changelog-v1.12.152.md) | 拓扑连接表先返回，连接池改为瞬时聚合 |
| [v1.12.151](changelog-v1.12.151.md) | 拓扑视图按 Pod 展示最近 7 天连接池和分析 |
| [v1.12.150](changelog-v1.12.150.md) | 拓扑视图按库筛选可以全选和反选 |
| [v1.12.149](changelog-v1.12.149.md) | 拓扑视图统计当前全部连接，排除复制账号 |
| [v1.12.148](changelog-v1.12.148.md) | 拓扑视图按库筛选可以输入搜索 |
| [v1.12.147](changelog-v1.12.147.md) | 拓扑视图可排序、按库筛选并导出 |
| [v1.12.146](changelog-v1.12.146.md) | 查询打字不再等整页，Kafka 删除改走工单，SQL 洞察增加拓扑 |
| [v1.12.145](changelog-v1.12.145.md) | 归档改由 SQL 执行器运行，查询页签按账号保留，登记账号须填现有密码 |
| [v1.12.144](changelog-v1.12.144.md) | Redis 域名可批量追加和撤销审核人 |
| [v1.12.143](changelog-v1.12.143.md) | 审核 SQL 可展开，主机峰值显示时间，PostgreSQL 对象 DDL 放行 |
| [v1.12.142](changelog-v1.12.142.md) | 主机智能分析增加 CPU、网络和连接数，PostgreSQL DDL 允许带标签的 DO 块 |
| [v1.12.141](changelog-v1.12.141.md) | 登录会话显示使用状态，工作台待办标题缩短 |
| [v1.12.140](changelog-v1.12.140.md) | Redis 按域名取地址和审核人，代审记入审批流程，首屏脚本 gzip |
| [v1.12.139](changelog-v1.12.139.md) | 海豚管理不再卡住，StarRocks 按每台 FE 探测目录 |
| [v1.12.138](changelog-v1.12.138.md) | 同步列表不再读取 CDC 正文，StarRocks 按节点回填目录 |
| [v1.12.137](changelog-v1.12.137.md) | 工单列表不再读取 SQL 正文，StarRocks 登记按域名取实例账号 |
| [v1.12.136](changelog-v1.12.136.md) | StarRocks 按域名复用实例，FE 管理改用实例密码，Redis 审核人与云 PG 相同 |
| [v1.12.135](changelog-v1.12.135.md) | 会话服务不可用不再登出，StarRocks 不再重复登记，域名列表显示创建时间 |
| [v1.12.134](changelog-v1.12.134.md) | PostgreSQL 总览增加智能分析，SR 控制台样式打进发布包 |
| [v1.12.133](changelog-v1.12.133.md) | 表空间采集只扫当前实例，登录按自然日续期，终端最长 8 小时 |
| [v1.12.132](changelog-v1.12.132.md) | 云 Kafka 列表展示主题、分区、磁盘、版本和集群类型 |
| [v1.12.131](changelog-v1.12.131.md) | 邮件异常不再挡住飞书和其它渠道 |
| [v1.12.130](changelog-v1.12.130.md) | 工单通知失败留审计，admin 参与飞书同步 |
| [v1.12.129](changelog-v1.12.129.md) | Redis 查询放开只读命令，自助注册需管理员审批 |
| [v1.12.128](changelog-v1.12.128.md) | 数据库查询在 HTTP 页面可运行，SQL 窗口增加注释快捷键 |
| [v1.12.127](changelog-v1.12.127.md) | 数据迁移审核人与建库同一审批人池，不再按任务租户拦截 |
| [v1.12.126](changelog-v1.12.126.md) | 工单通知写入查询审计，云资源增加 Kafka，云同步按账号并行 |
| [v1.12.125](changelog-v1.12.125.md) | 失败或已终止的工单退回待审批，不再直接重跑；SQL 洞察页签改为快照查询 |
| [v1.12.124](changelog-v1.12.124.md) | SQL 执行器增加协议号，须用本版包重新下发；旧执行器不再带着旧拆句执行 |
| [v1.12.123](changelog-v1.12.123.md) | 主机总览增加系统盘使用率和磁盘增长分析，报告可保存并单独打开 |
| [v1.12.122](changelog-v1.12.122.md) | 实例总览主机显示应用名，过滤后分页固定在表格底部 |
| [v1.12.121](changelog-v1.12.121.md) | 监控同步按键补齐主机标签，主机总览默认每页 10 条 |
| [v1.12.120](changelog-v1.12.120.md) | 云主机磁盘按块展示，实例总览纳入主机规格和数据盘使用率 |
| [v1.12.119](changelog-v1.12.119.md) | 查询过滤切窗口不丢，每页条数可改，同步列表显示任务源端地址 |
| [v1.12.118](changelog-v1.12.118.md) | 主机按连接地址去重并关联云主机，查询编辑器复制粘贴可用 |
| [v1.12.117](changelog-v1.12.117.md) | StarRocks 容量对齐监控，标签按租户去重，批量重启可弹出确认 |
| [v1.12.116](changelog-v1.12.116.md) | 实例总览按云购买磁盘计算 PostgreSQL 容量和使用率 |
| [v1.12.115](changelog-v1.12.115.md) | 实例总览纳入授权实例，同步和迁移按钮按角色授权 |
| [v1.12.114](changelog-v1.12.114.md) | SQL 洞察页签按角色授权，工单拒绝不完整 INSERT |
| [v1.12.113](changelog-v1.12.113.md) | 实例总览采集 StarRocks，SQL 审核保持 INSERT SELECT 为一条语句 |
| [v1.12.112](changelog-v1.12.112.md) | SQL 洞察增加慢日志同步监控，单页超时不再整次失败 |
| [v1.12.111](changelog-v1.12.111.md) | 云 PostgreSQL 变配合并扩盘，慢日志可走独立 Kafka，查询不再重复拼接 LIMIT |
| [v1.12.110](changelog-v1.12.110.md) | 云资源对账说明账号不一致，补齐 PG 冻结年龄知识 |
| [v1.12.109](changelog-v1.12.109.md) | 查询窗口按表名和字段名中间的内容提示 |
| [v1.12.108](changelog-v1.12.108.md) | 查询窗口在 WHERE 等关键字后面提示字段 |
| [v1.12.107](changelog-v1.12.107.md) | CDC 修复源端的地址下拉显示实例名，可按地址或实例名筛选 |
| [v1.12.106](changelog-v1.12.106.md) | 查询补全按库名点号提示表，PAT 在 StarRocks 上放行其余 SHOW |
| [v1.12.105](changelog-v1.12.105.md) | PAT 在 StarRocks 上允许只读 SHOW，账号和导出语句仍拒绝 |
| [v1.12.104](changelog-v1.12.104.md) | 同步列表任务名改为 publication，查询字段提示带注释 |
| [v1.12.103](changelog-v1.12.103.md) | 同步列表显示库绑定域名，CDC 源端按地址找回实例 |
| [v1.12.102](changelog-v1.12.102.md) | 同步列表展示库绑定域名，采集不按域名列表拨号，重启改回连接主机 |
| [v1.12.101](changelog-v1.12.101.md) | 查询窗口提示表和字段，同步槽重启读回已采集的连接器 |
| [v1.12.100](changelog-v1.12.100.md) | Agent 页签整理工具栏，SQL 执行器显示运行状态 |
| [v1.12.99](changelog-v1.12.99.md) | PAT 收紧敏感接口并限制来源地址 |
| [v1.12.98](changelog-v1.12.98.md) | SQL 执行器页签把登记列表放在下发记录前面 |
| [v1.12.97](changelog-v1.12.97.md) | Agent 集中到配置中心，PAT 查询写入审计并限制只读，PostgreSQL 函数先于触发器 |
| [v1.12.96](changelog-v1.12.96.md) | 终端写明 SSH 失败原因，RSA 兼容旧算法，CDC 源库可过滤 |
| [v1.12.95](changelog-v1.12.95.md) | 查询页同名库的环境列表按名称排序 |
| [v1.12.94](changelog-v1.12.94.md) | 查询页同名库的相同环境带上域名以便区分 |
| [v1.12.93](changelog-v1.12.93.md) | 同步列表按连接主机采集，批量重启使用已记下的连接器 |
| [v1.12.92](changelog-v1.12.92.md) | 工单和同步槽按域名拨号，云资源展示内网地址，CDC 草稿恢复后留在原步骤 |
| [v1.12.91](changelog-v1.12.91.md) | CDC 源端改用域名，连接器可授权划拨，Kafka 批量授权，SQL 执行器显示运行状态 |
| [v1.12.90](changelog-v1.12.90.md) | 云资源按内网地址绑定，实例批量删除，查询结果按白名单导出 |
| [v1.12.89](changelog-v1.12.89.md) | 查询结果的大整数按十进制原文返回，避免末尾被改掉 |
| [v1.12.88](changelog-v1.12.88.md) | 工单详情审核后自动刷新，查询页点选环境，建库审批人收窄 |
| [v1.12.87](changelog-v1.12.87.md) | 数据迁移补齐中转与定时，同步槽采集补齐 Connect，权限申请列出现有账号 |
| [v1.12.86](changelog-v1.12.86.md) | 查询页签、收藏和历史按当前租户分开 |
| [v1.12.85](changelog-v1.12.85.md) | 查询页铺满屏幕，同名结果列分开显示，库目录预取并建租户索引 |
| [v1.12.84](changelog-v1.12.84.md) | 登录日志内网地址优先用本机上报，没有上报时回退请求里的内网地址 |
| [v1.12.83](changelog-v1.12.83.md) | 登录日志分开记录公网地址和内网地址 |
| [v1.12.82](changelog-v1.12.82.md) | 查询运行按本次选区、搜索框不再被清掉、打开收藏后再运行有反馈 |
| [v1.12.81](changelog-v1.12.81.md) | 查询收藏与选中执行、云账号多地域、CDC 分库初始化 |
| [v1.12.80](changelog-v1.12.80.md) | DDL 数据源筛选只按域名匹配 |
| [v1.12.79](changelog-v1.12.79.md) | DDL 筛选不再清掉已选数据源；DML 矮屏时提交按钮固定在左侧栏底 |
| [v1.12.78](changelog-v1.12.78.md) | CDC 审核人收窄、加表源端域名/初始化信号/REPLICA IDENTITY、巡检认目标端；工单数据源只显示地址。须重下发 sql-agent |
| [v1.12.77](changelog-v1.12.77.md) | 加表审核人支持已授权资源归属租户与超级用户 |
| [v1.12.76](changelog-v1.12.76.md) | 审计会话加表按列表范围认父任务，子任务继承父租户 |
| [v1.12.75](changelog-v1.12.75.md) | 批量加表对齐 tables、创建 0 明细、meta 合并减负 |
| [v1.12.74](changelog-v1.12.74.md) | 批量加表「可加」展示缺表/占用源库明细 |
| [v1.12.73](changelog-v1.12.73.md) | 旧 sql-agent 缺 meta 提示修正；可加标签父任务口径；平台 Agent 后台轮询减负 |
| [v1.12.72](changelog-v1.12.72.md) | CDC 加表下一步性能；sql-agent `/v1/sql/meta` 就近拉表 |
| [v1.12.71](changelog-v1.12.71.md) | 划拨后按任务用源实例；加表核对 publication、批量可选表 |
| [v1.12.70](changelog-v1.12.70.md) | CDC 任务可划拨到指定租户（含加表等子任务） |
| [v1.12.69](changelog-v1.12.69.md) | CDC 源端只展示生效地址；修复不改实例 host，源库连接跟随任务地址 |
| [v1.12.68](changelog-v1.12.68.md) | CDC 批量加表按父任务链路名；Debezium 按库合并分表 transforms |
| [v1.12.67](changelog-v1.12.67.md) | 划拨/授域名后工单与已登记库可见外租库；实例列表并入 domain 父实例 |
| [v1.12.66](changelog-v1.12.66.md) | 迁租后 `/auth/me` 自动换票愈合僵死 JWT |
| [v1.12.65](changelog-v1.12.65.md) | 工单库可见：domain/instance 授权展开已登记库；空 domains 前端放行 |
| [v1.12.64](changelog-v1.12.64.md) | CDC 加表分表组 regex include.list + ByLogicalTableRouter（与创建 init 一致） |
| [v1.12.63](changelog-v1.12.63.md) | CDC 跨实例批量加表（上限 128）、已同步占用、审批开关、分表扩展、batch 404 降级 |
| [v1.12.62](changelog-v1.12.62.md) | 客户端授权选用户；租户授权资源过滤；CDC 父任务过滤；DQL 原生下载 |
| [v1.12.61](changelog-v1.12.61.md) | DQL 导出 gzip；须同步升级 sql-agent |
| [v1.12.60](changelog-v1.12.60.md) | DDL 数据源下拉过滤与全选/反选 |
| [v1.12.59](changelog-v1.12.59.md) | DQL 下载流程单列展示与白话步骤；执行器地址/本机直连提示 |
| [v1.12.58](changelog-v1.12.58.md) | DQL 异步导出与下载流程；sql-agent export-jobs；发版冻结提交；SQL 多行拆分修复 |
| [v1.12.57](changelog-v1.12.57.md) | PG 权限申请/建库授权；跨租用户授权；工单可见校验；Nacos 磁盘缓存回退 |
| [v1.12.56](changelog-v1.12.56.md) | 系统通知；跨租批量授权；划拨迁 grant；资源申请/工作台含 dba |
| [v1.12.55](changelog-v1.12.55.md) | 批量审核人并发；撤销拆逗号脏名；超时复核；库列表按租户筛 |
| [v1.12.54](changelog-v1.12.54.md) | 终端失效不重连刷屏；访问日志脱敏/限频；已登记库分页可改条数 |
| [v1.12.53](changelog-v1.12.53.md) | 已登记库批量撤销审核人（差集，可撤空，含孤儿名） |
| [v1.12.52](changelog-v1.12.52.md) | 库有效租户统一解析；批量审核人重试；域名按租户筛选 |
| [v1.12.51](changelog-v1.12.51.md) | 分库审核人解析对齐前端；批量追加审核人写后回读与重试 |
| [v1.12.50](changelog-v1.12.50.md) | 库划拨/追加审核人按目标租户成员过滤合并 |
| [v1.12.49](changelog-v1.12.49.md) | Nacos 静态资源本地加速；DDL 审核人并集；DQL 导出 limit；Redis CPU 迷你曲线 |
| [v1.12.48](changelog-v1.12.48.md) | 已有库幂等补齐 Nacos 表与侧栏菜单 |
| [v1.12.47](changelog-v1.12.47.md) | Nacos 控制台代登；按环境 SSH 密钥档案；云资源与 Agent 下发增强 |
| [v1.12.46](changelog-v1.12.46.md) | 修复 PostgreSQL DML 占死连接导致不落库；执行器可停远端与重新下发 |
| [v1.12.45](changelog-v1.12.45.md) | 客户端访问授权与云资源；工单远程进度细化；安装包仍带 sql-agent |
| [v1.12.44](changelog-v1.12.44.md) | 下发 SQL 执行器直接使用包内 sql-agent，不再在生产机编译 |
| [v1.12.43](changelog-v1.12.43.md) | 执行器从下发成功记录登记；安装包仍带 sql-agent |
| [v1.12.42](changelog-v1.12.42.md) | SQL 执行器随一体包下发；本环境直连，其它环境走执行器 |
| [v1.12.41](changelog-v1.12.41.md) | 工单执行显示进度并可终止；主机同步补区域与环境 |
| [v1.12.40](changelog-v1.12.40.md) | 有效会话自动续期；DDL 分库失败后继续其余库 |
| [v1.12.39](changelog-v1.12.39.md) | PostgreSQL DDL 分库按前缀现查并逐库执行，重试按库跳过 |
| [v1.12.38](changelog-v1.12.38.md) | DDL 约束检查可关闭；StarRocks 路径走全局默认；实例总览整页滚动 |
| [v1.12.37](changelog-v1.12.37.md) | 同步列表增加「刷新」，重新加载复制槽列表且不触发采集 |
| [v1.12.36](changelog-v1.12.36.md) | Redis 查询页签从左侧起排，页签名只显示前 10 个字符 |
| [v1.12.35](changelog-v1.12.35.md) | Redis 查询按域名开页签；补发 v1.12.34 未能挂上的安装包 |
| [v1.12.34](changelog-v1.12.34.md) | 代码已入库。标签被空的不可变 Release 占用，安装包以 v1.12.35 发布 |
| [v1.12.33](changelog-v1.12.33.md) | 实例总览表格在页内滚动，筛选、汇总和分页留在视口内 |
| [v1.12.32](changelog-v1.12.32.md) | 加表沿用父 Connector、StarRocks 列映射、PostgreSQL 迁移补序列与约束、助手只读 SQL |
| [v1.12.31](changelog-v1.12.31.md) | 告警实时动态、CDC 编排加固与修复源端、域名/已登记库删除 |
| [v1.12.30](changelog-v1.12.30.md) | CDC Debezium 配置页查看为「编辑」、提交为「保存」 |
| [v1.12.29](changelog-v1.12.29.md) | 工单申请页按生产/测试过滤数据源（`ticket.env_groups`） |
| [v1.12.28](changelog-v1.12.28.md) | DNS 管理与变更工单、实例总览存储使用率、审核规则 JSON、改密踢会话 |
| [v1.12.27](changelog-v1.12.27.md) | 整页 AI 助手、实例总览、复制槽按 slot_name 去重、操作日志分页 |
| [v1.12.26](changelog-v1.12.26.md) | 同步列表批量重启对齐 Hub（heartbeat + task0）、操作日志持久化、采集保留 id |
| [v1.12.25](changelog-v1.12.25.md) | 备份恢复工单、CDC MySQL、MySQL 慢 SQL/会话、工单预约执行、HA/闪回/K8s 缺口补齐 |
| [v1.12.24](changelog-v1.12.24.md) | 工单 SQL 美化/高亮、检测分页与状态过滤、缺分号规则、执行明细全文 |
| [v1.12.23](changelog-v1.12.23.md) | K8s 容器管理骨架；CDC 异常判定对齐老平台 |
| [v1.12.22](changelog-v1.12.22.md) | 定时任务中心；CDC 分表网格；K8s 方案文档 |
| [v1.12.21](changelog-v1.12.21.md) | CDC 列表瘦身与结构巡检；crypto_secret 热更与启动抽检 |
| [v1.12.20](changelog-v1.12.20.md) | CDC 详情页签标题；任务 id 支持 Hub 迁移非 UUID |
| [v1.12.19](changelog-v1.12.19.md) | CDC 列表按页探测链路；工单列表补环境/类型；DQL 已下载与导出超时 |
| [v1.12.18](changelog-v1.12.18.md) | 复制槽真实采集与筛选；CDC 按页签筛状态；连接器与用户名搜索；显示名去掉工号 |
| [v1.12.17](changelog-v1.12.17.md) | 用户管理同步飞书身份；工单私聊不再用企业邮箱 |
| [v1.12.16](changelog-v1.12.16.md) | 工单提交回列表并关页签；SMTP 支持 LOGIN 与发信限速 |
| [v1.12.15](changelog-v1.12.15.md) | 告警只打已登记资源；WITH DML 不误拆；1=1 独立开关 |
| [v1.12.14](changelog-v1.12.14.md) | admin/dba 看全平台监控；dba 不可删；租户页与图例体验 |
| [v1.12.13](changelog-v1.12.13.md) | 告警历史分页加速、关联通知、表格自适应 |
| [v1.12.12](changelog-v1.12.12.md) | 告警通知补 host/目录；历史按通知次数 + 恢复行 |
| [v1.12.11](changelog-v1.12.11.md) | Redis 工单审批通过后自动后台执行 |
| [v1.12.10](changelog-v1.12.10.md) | 已登记库列表体验、首屏加速与顶栏 Tab 保活 |
| [v1.12.9](changelog-v1.12.9.md) | 已登记库批量设置审核人/可访问用户 |
| [v1.12.8](changelog-v1.12.8.md) | 退出登录不再弹 unauthorized |
| [v1.12.7](changelog-v1.12.7.md) | 同一账号只保留最新登录会话 |
| [v1.12.6](changelog-v1.12.6.md) | 飞书接收人识别与回退；工单通知审核人；建库成功私聊申请人 |
| [v1.12.5](changelog-v1.12.5.md) | SR 登记路径探测/注销；Alive 状态；监控标签隔离；可选登录会话 |
| [v1.12.4](changelog-v1.12.4.md) | 一体/后端包与 Docker 镜像打入 Node/DB/Platform Agent |
| [v1.12.3](changelog-v1.12.3.md) | 侧栏收窄；查询 alias 去括号；类型 Tab 固定 6 个 +「…」 |
| [v1.12.2](changelog-v1.12.2.md) | 查询树只显示 tags 的 `alias=`（兼容 `label=`）；登记页标签说明 |
| [v1.12.1](changelog-v1.12.1.md) | 已登记库环境/域名筛选；域名按库明细展示 |
| [v1.12.0](changelog-v1.12.0.md) | Nacos 通知渠道 + 群机器人方向页配；告警全局频控与恢复必达；查询收藏/脱敏等 |
| [v1.11.0](changelog-v1.11.0.md) | 平台资源申请、跨租审核、LDAP 锁定解锁 |
| [v1.0.0](https://github.com/dbaspace/auto-dba/releases/tag/v1.0.0) | 早期一体/分服务产物包基线 |
| [v1.0.1](https://github.com/dbaspace/auto-dba/releases/tag/v1.0.1) | 早期补丁（若仍保留该标签） |
| 历史 v1.1～v1.10 | Release 已下线，要点见 [changelog-v1.md](changelog-v1.md) |
| 历史 v2.x | GitHub Release/标签已下线；说明文档仍见仓库内 `docs/changelog-v2.*.md` |

## 方式 A：一体包（推荐 CentOS 手工）

```bash
# 构建机
./scripts/pack_release.sh all
scp dist/auto-dba-release-linux-amd64-*.tgz user@host:/tmp/

# 运行机
sudo mkdir -p /opt/auto-dba
sudo tar -xzf /tmp/auto-dba-release-linux-amd64-*.tgz -C /opt/auto-dba --strip-components=1
cd /opt/auto-dba
```

配置（Nacos DataId 或本地 `config/config.yaml`）：

```yaml
http:
  host: "0.0.0.0"
  port: 8800
  serve_static: true
```

```bash
cd /opt/auto-dba   # 必须在包根，才能解析 frontend/dist
./bin/auto-dba -config config/config.yaml
# 浏览器 → http://服务器:8800/
```

冒烟（静态与标签图标）：

```bash
curl -sI http://127.0.0.1:8800/favicon.svg   # 期望 200
curl -sI http://127.0.0.1:8800/favicon.ico   # 期望 200
# 经域名若非 200：按 docs/faq-spa-white-screen.md 查 APISIX 是否转发 /favicon.*
```

包内结构：

```text
bin/auto-dba
frontend/dist/          # 含 favicon.svg / favicon.ico / apple-touch-icon.png
scripts/terminal/       # 主机 Web 终端 SSH 脚本（node-terminal.sh）
assets/terminal/        # 终端页静态资源
config/                 # 示例 YAML
README-RELEASE.txt
```

> 须在**包根**启动。缺 `scripts/terminal/node-terminal.sh` 时打开主机终端会报「终端脚本不存在」；见 [terminal.md](terminal.md)。

## 方式 B：仅后端

```bash
./scripts/pack_release.sh backend
# 解压后
./bin/auto-dba -config config/config.yaml
# serve_static: false
# 页面：本机 pnpm -C frontend dev → :5174，或另用方式 C
```

## 方式 C：仅前端 + Nginx

```bash
./scripts/pack_release.sh frontend
# 解压后得到 dist/ + nginx.conf.example
```

1. 后端单独部署（方式 B），`serve_static: false`，监听 `127.0.0.1:8800` 或内网
2. Nginx `root` 指向前端包的 `dist/`，`/api/` 反代后端（见 [`deploy/nginx.conf.example`](../deploy/nginx.conf.example)）
3. 浏览器访问 Nginx 端口（如 80），不是 5174

## 方式 D：Docker

```bash
./scripts/pack_release.sh docker
# 或
docker build -f deploy/Dockerfile -t auto-dba:latest .

docker run --rm -p 8800:8800 \
  -v /path/to/config.yaml:/app/config/config.yaml \
  auto-dba:latest
```

镜像内已含 `frontend/dist` 与 `deploy/*/bin` 三类 Agent；请保证配置中 `http.serve_static: true`（Dockerfile 会在示例 config 末尾追加提示）。密钥、DSN 建议走 Nacos 或挂载配置，勿打进镜像。工作目录为 `/app`，与 SSH 下发解析路径一致。

当前镜像基于 Alpine，适合较新内核环境；若必须在极老主机跑容器，优先用方式 A 的静态二进制 + `CGO_ENABLED=0`。

## 一体部署白屏排查

完整 FAQ（含 APISIX `404 Route Not Found`、304 缓存、`serve_static` / cwd）：**[faq-spa-white-screen.md](faq-spa-white-screen.md)**。

摘要：

1. 强刷 / 清缓存；包根启动；`serve_static: true`；看 `boot: serve_static ... index=ok`。
2. 经域名访问时，若 `/assets/*.js` 响应为 `{"error_msg":"404 Route Not Found"}` → **改网关路由**，把 `/assets/*`（及 `/`）转到后端 8800。
3. 自检：

```bash
curl -sI http://host:8800/ | grep -i cache-control   # 期望 no-cache
curl -sI http://host:8800/assets/不存在的旧hash.js   # 期望 404，非 HTML
```

## 配置与文档

- 白屏 FAQ：[faq-spa-white-screen.md](faq-spa-white-screen.md)
- Nacos / 热更：[nacos-config.md](nacos-config.md)
- 前端构建与运行分离背景：[frontend-deploy.md](frontend-deploy.md)（细节已并入本文，该文保留为短链）
- 开发启动：[README](../README.md)

## 不要做

- 在 CentOS 7 上装 Node 硬扛 `pnpm build`
- 生产跑 `pnpm dev` / `vite preview`
- 一体部署时仍去找 5174 端口
