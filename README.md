<p align="center">
  <img src="docs/assets/keeplus-lockup.svg" alt="KeePlus" width="280" />
</p>

# KeePlus

**KeePlus** 是面向 DBA 与开发的数据库运维自助工作台（界面品牌名）。覆盖工单、查询、监控诊断、CDC、迁移、闪回、归档、集群部署、告警、多租户与权限治理。业务对象登记在平台内，支持本地登录（可接 LDAP / SSO）。

> **命名说明**：产品 / 界面称 **KeePlus**；本仓库、Go 模块、二进制与部分路径仍为工程名 **`auto-dba`**（如 `./bin/auto-dba`、`cmd/auto-dba`、Nacos DataId `auto-dba.yaml`）。文档中二者并存时：对人读的产品能力用 KeePlus，对命令与配置键用 `auto-dba`。Logo 资源：[docs/assets/keeplus-logo.svg](docs/assets/keeplus-logo.svg)（与前端 `favicon.svg` / 侧栏图标一致）。

**日常操作**（多租户、登记、工单、审计划拨等）见 [docs/ops-manual.md](docs/ops-manual.md)。功能按侧栏逐项说明见 [docs/features.md](docs/features.md)。模块关系总览见 [docs/architecture.html](docs/architecture.html)。集群部署（StarRocks）方案见 [docs/sr-cluster.md](docs/sr-cluster.md)。

前端为 **React 18 + Ant Design 5 + Redux Toolkit + ahooks + react-router-dom v6 + Tailwind 3**（目录 `frontend/`）。本地为**前后端分离**：改前端不必动后端，改后端不必重新构建前端。

## 能力概览

核心业务能力：

| 能力 | 说明 | 文档 |
|------|------|------|
| **工单** | DDL / DML / DQL / 建库 / 权限申请 / Kafka·Redis 变更；审批、影响说明、后台执行与结果收敛 | [docs/features.md](docs/features.md#工单) |
| **数据查询** | 已登记库只读 SQL、Kafka Topic 抽样、Redis 命令探查；写入查询审计 | [docs/features.md](docs/features.md#数据查询) |
| **监控与诊断** | SQL 洞察 / AWR / 表空间；会话与锁；告警 Ruler（规则、静默、路由、值班） | [docs/features.md](docs/features.md#诊断)、[docs/alerts.md](docs/alerts.md) |
| **同步与迁移** | CDC（PG→Kafka→SR）、数据迁移、表结构对比、复制槽 | [docs/features.md](docs/features.md#同步与迁移)、[docs/cdc-reinit.md](docs/cdc-reinit.md) |
| **数据安全** | 备份（逻辑/物理 Agent）、PG 闪回、归档与脱敏 | [docs/backup.md](docs/backup.md)、[docs/features.md](docs/features.md#数据安全) |
| **集群与 HA** | StarRocks / MySQL·PG·Redis 部署工作台；接入层代理；高可用维护与切换 | [docs/sr-cluster.md](docs/sr-cluster.md)、[docs/db-cluster.md](docs/db-cluster.md)、[docs/ha.md](docs/ha.md) |
| **配置与 CMDB** | 实例 / 已登记库 / 账号；区域环境；资产目录、拓扑、发现登记 | [docs/features.md](docs/features.md#数据库配置)、[docs/cmdb.md](docs/cmdb.md) |

平台与治理：

| 能力 | 说明 | 文档 |
|------|------|------|
| **多租户** | 行级 `tenant_id` 隔离；登录选租户；成员角色驱动菜单；私有 ACL；跨租 grant；审计划拨（含库批量指定租户） | [docs/tenancy.md](docs/tenancy.md) |
| **认证与权限** | 本地 / LDAP / SSO；菜单与操作 URN；LDAP·SSO 首次登录默认 developer + 默认租户 | [docs/features.md](docs/features.md#登录与账号)、[docs/rbac-permissions.md](docs/rbac-permissions.md) |
| **Nacos 配置** | 生产可将 `db.dsn` / `jwt_secret` / SSH 私钥放到 Nacos；本地只留引导；支持热更。Namespace = 平台部署，≠ 业务「环境管理」 | [docs/nacos-config.md](docs/nacos-config.md) |

完整侧栏逐页说明见 [docs/features.md](docs/features.md)。

## 运行方式：手动维护

本项目**不常驻、不自动拉起**前后端。开发、联调、验收时由维护人按下方命令自行启动；用完自行停止。Agent / 脚本不要默认帮你 `start`。

停止（按端口杀进程即可）：

```bash
# 后端 8800
lsof -tiTCP:8800 -sTCP:LISTEN | xargs kill 2>/dev/null || true

# 前端 Vite 5174 / 5175
lsof -tiTCP:5174 -sTCP:LISTEN | xargs kill 2>/dev/null || true
lsof -tiTCP:5175 -sTCP:LISTEN | xargs kill 2>/dev/null || true
```

## 需要时手动启动

两个进程互不影响，请分别启动、分别重启。用完记得停掉，不要挂着。

**后端（只提供 API）**

```bash
# 推荐：增量编译后启动（比反复 go run 快）
./scripts/dev-backend.sh

# 或手动
go build -o bin/auto-dba ./cmd/auto-dba
./bin/auto-dba -config config/config.yaml start
```

默认监听 `0.0.0.0:8800`。健康检查：`GET /healthz`。  
`http.serve_static` 默认为 `false`，**不要用 8800 打开页面**。

元数据使用 PostgreSQL。复制 `config/config.example.yaml` 为本地 `config/config.yaml`。生产可将 `db.dsn`、`jwt_secret`、SSH 私钥放到 **Nacos**（见 [docs/nacos-config.md](docs/nacos-config.md)，含 [业务环境 vs Nacos Namespace](docs/nacos-config.md#业务环境-vs-nacos-namespace方案-a)；引导片段见 [config/bootstrap.example.yaml](config/bootstrap.example.yaml)），本地只写 `nacos:` 引导；`enabled: false` 时仍只读本地 YAML。该文件和 `config/terminal/` 已忽略，不要提交账号密码或密钥。Nacos Namespace 对应平台部署（开发/生产），不是配置中心里的 `beijing`/`test` 等业务环境编码。  
启动时按 `schema_version` / `seed_version` 做引导：版本未变则跳过全量 migrate/seed（二次启动通常亚秒～2s 级）；改了 `migrate.go`/`seed.go` 需递增 `internal/store/bootstrap.go` 中的版本常量，或设 `AUTO_DBA_FORCE_BOOTSTRAP=1` 强制重跑。账号无 CREATEDB 权限，因此不会自动创建新库。

首次启动会种子：

- 用户 `admin` / `admin123`
- 全局参数（`sync_default_approver=admin`、`sync_approval_enabled`、SQL 约束开关）
- 示例实例 `127.0.0.1:5432` + 已登记库 `appdb`（审批人 `admin`）

**前端（只提供页面）**

包管理用 **pnpm**，样式底层用 **Tailwind CSS**（`preflight: false`，不冲 Ant Design），在仓库根目录用 `pnpm -C` 指定子项目启动：

```bash
# 首次安装依赖
pnpm -C ./frontend install

# 启动开发服务
pnpm -C ./frontend dev
```

也可直接用根目录快捷脚本（内部同样是 `pnpm -C ./frontend …`）：

```bash
pnpm run install:web
pnpm run dev
```

Vite 默认 http://localhost:5174 ，`/api` 代理到 8800。若 5174 已被占用，会自动改用 **5175**（以终端打印的 Local 地址为准）。Token 存在 **localStorage**。

登录、工单、闪回请只在这个地址操作。改 React 热更新即可，**不要**为此重启 8800，也**不必** `pnpm -C ./frontend build`。

改 Go 后只重启后端；前端标签页不用关。

全链路监控图组件为 `PipelineVisualization.tsx`（Tailwind class + Ant Card 外观），小屏横滚、大屏 `w-max min-w-[1080px]` 保持三块布局不散。

## 约束说明

提交前必须在「配置 → 实例 / 已登记库」登记目标，否则返回可读错误（库未登记 / 非审批人）。  
`issue_type` 使用 varchar，不再依赖 PostgreSQL ENUM。  
CDC / 迁移 / 海豚 / SR 在 `config.yaml` 未配置外部地址时任务可落库，执行返回「未配置」而不是 panic。

PG 闪回生产约定：默认不改 `REPLICA IDENTITY`；DEFAULT 表靠 FPW/堆页死行补齐非主键列（VACUUM 前完成）；仅高风险表在变更窗临时 FULL。

## 相关文档

| 文档 | 内容 |
|------|------|
| [docs/ops-manual.md](docs/ops-manual.md) | **操作手册**：登录选租户、多租户、登记、工单、划拨、FAQ |
| [docs/assets/keeplus-logo.svg](docs/assets/keeplus-logo.svg) | 产品 Logo（方标）；[lockup](docs/assets/keeplus-lockup.svg) 为图标+字标 |
| [docs/features.md](docs/features.md) | 侧栏功能目录与页面说明（含多租户、登录、告警入口） |
| [docs/tenancy.md](docs/tenancy.md) | 多租户模型、ACL、划拨、grant、API、防串租清单 |
| [docs/nacos-config.md](docs/nacos-config.md) | Nacos 部署、Namespace、热更、与业务环境区分 |
| [docs/alerts.md](docs/alerts.md) | 告警 Ruler / 规则 / 静默 / 路由 / 值班 |
| [docs/rbac-permissions.md](docs/rbac-permissions.md) | 新菜单与操作权限登记（前后端 URN） |
| [docs/migration-schema-compare.md](docs/migration-schema-compare.md) | 表结构对比 |
| [docs/sr-cluster.md](docs/sr-cluster.md) / [docs/db-cluster.md](docs/db-cluster.md) / [docs/ha.md](docs/ha.md) | 集群部署与高可用 |
| [docs/architecture.html](docs/architecture.html) | 模块关系总览 |

配置引导示例：[config/config.example.yaml](config/config.example.yaml)、[config/bootstrap.example.yaml](config/bootstrap.example.yaml)（Nacos）。
