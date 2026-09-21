# auto-dba

独立的 DBA 运维自助工作台，覆盖工单、建库、CDC、迁移、闪回、归档等能力。使用本地登录、本地实例库表与审批人。

前端为 **React 18 + Ant Design 5 + Redux Toolkit + ahooks + react-router-dom v6 + Tailwind 3**（目录 `frontend/`）。本地为**前后端分离**：改前端不必动后端，改后端不必重新构建前端。

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
go build -o bin/auto-dba ./cmd/auto-dba
./bin/auto-dba -config config/config.yaml start
```

默认监听 `0.0.0.0:8800`。健康检查：`GET /healthz`。  
`http.serve_static` 默认为 `false`，**不要用 8800 打开页面**。

元数据使用 PostgreSQL（见 `config/config.yaml` 的 `db.dsn`，当前库为 `db_autosql`）。启动时自动建表并写入种子数据。账号无 CREATEDB 权限，因此不会自动创建新库。

首次启动会种子：

- 用户 `admin` / `admin`
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

## 表结构对比

菜单 **同步与迁移 → 表结构对比**（`/ops/migration/compare`）：对同族实例（PostgreSQL 互比，或 MySQL/TiDB/OceanBase 互比）做异步结构对比，并生成源→目标、只增不删的修复 SQL。

新增页面及页面上的新建/编辑/删除等操作，须在菜单中登记并完成前后端鉴权，见 [docs/rbac-permissions.md](docs/rbac-permissions.md)。

说明、对比范围、API 与自测见 [docs/migration-schema-compare.md](docs/migration-schema-compare.md)。
