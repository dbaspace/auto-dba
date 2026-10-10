# Nacos 核心配置

平台启动时可选从 Nacos 拉取业务配置（元库 DSN、`jwt_secret`、SSH 私钥等），本地文件只保留**如何连接** Nacos。  
`auto-dba` 与 `db-monitor` 共用 `config.Load`，同一 DataId。

引导示例：[`config/bootstrap.example.yaml`](../config/bootstrap.example.yaml)、[`config/config.example.yaml`](../config/config.example.yaml)。仓库总览入口见 [README](../README.md#能力概览近期重点)。

> **与多租户无关**：Nacos Namespace 表示「这一套进程连哪份平台配置」（开发/生产）。业务侧的租户隔离见 [tenancy.md](tenancy.md)；「环境管理」里的 `test`/`beijing` 等也不是 Nacos Namespace。

## 单机 Nacos 部署（Docker，可选自建）

现网已有 `10.100.113.151:8848` 时可跳过。本地或新环境自建：

```bash
docker run -d --name nacos-standalone \
  -e MODE=standalone \
  -e NACOS_AUTH_ENABLE=true \
  -p 8848:8848 \
  -p 9848:9848 \
  nacos/nacos-server:v2.3.2
```

| 端口 | 用途 |
|------|------|
| 8848 | 控制台 / HTTP |
| 9848 | Nacos 2.x 客户端 gRPC（SDK 会用，防火墙需放行） |

- 控制台：`http://127.0.0.1:8848/nacos`（默认 `nacos` / `nacos`，上线前改密）
- 也可用官方发行包 `startup.sh -m standalone`
- 日后切集群：应用侧只把 `server_addr` 改成逗号多地址，业务代码不变

### 控制台建 Namespace 与 DataId

1. 「命名空间」→ 新建（如名称 `autosql`），复制生成的 **Namespace ID**
2. 左上角切到该空间 →「配置管理」→「配置列表」→「+」
3. Data ID=`auto-dba.yaml`，Group=`DEFAULT_GROUP`，格式=YAML，粘贴正文后**发布**
4. 本地 `nacos.namespace` 填该 ID（不要填显示名）；`public` 填 `""` 或 `public`

## 当前环境（单机）

| 项 | 值 |
|----|-----|
| 地址 | `10.100.113.151:8848`（控制台 / HTTP；gRPC 另用 9848） |
| 命名空间名称 | `autosql`（控制台显示名） |
| Namespace ID | `e4dec2e2-4711-432f-9110-cc9b67d66b81`（本地 `nacos.namespace` 填这个） |
| Group | `DEFAULT_GROUP` |
| Data ID | `auto-dba.yaml` |

控制台：`http://10.100.113.151:8848/nacos`

### `nacos.namespace` 怎么填

客户端只认 **Namespace ID**，与控制台「命名空间」表第二列一致：

| 控制台名称 | 本地 `namespace` 写法 |
|------------|------------------------|
| **public**（默认空间） | `""`（空）或字面量 `public`（代码会当成空串） |
| **autosql**（本项目） | `e4dec2e2-4711-432f-9110-cc9b67d66b81` |

生产密钥应放在 **autosql**，不要放 public。控制台左上角先切到对应空间再编辑 DataId。

## 业务环境 vs Nacos Namespace（方案 A）

平台里有两套容易混淆的「环境」，**不要混用**：

| 概念 | 存在哪里 | 作用 |
|------|----------|------|
| **业务环境**（配置中心 → 环境管理：`test` / `beijing` / `ai-beijing` 等） | 元库 `tbl_environments` | 实例 / 区域 / 工单的标签与筛选；**一套 auto-dba 同时服务多个** |
| **平台部署配置** | Nacos **Namespace** + 本地 `nacos.namespace` | 本进程的 `jwt_secret` / `db.dsn` / `http.port` 等；**一套进程只连一个 Namespace** |

推荐用法：

1. 每个**平台部署**（开发 / 预发 / 生产）建一个 Nacos Namespace（显示名如 `auto-dba-dev`、`autosql`），记下 **Namespace ID**。
2. 各 Namespace 下维护同名 DataId `auto-dba.yaml`（Group `DEFAULT_GROUP`），内容按该部署分别填 DSN、JWT、端口。
3. 该部署机器上的 `config/config.yaml` 里 `nacos.namespace` 填**该部署的 Namespace ID**，不要填业务环境 `code`。
4. 业务环境继续只在「环境管理」维护；与 Nacos、热更、Vite 代理无关。

反例：

- 不要为 `beijing`、`test`、`frankfurt` 等业务 code 各建一个 Nacos Namespace（与「单进程多业务环境」模型冲突，密钥/DSN 也会被错误拆散）。
- 不要把环境管理里的编码写进 `nacos.namespace`。

当前现网：Nacos 空间 `autosql`（ID `e4dec2e2-…`）对应**这一套平台**的配置；元库里的 `test` / `beijing` 等只是数据标签。

## 本地引导

`config/config.yaml` 顶部（参考 `config/config.example.yaml`）：

```yaml
nacos:
  enabled: true
  server_addr: "10.100.113.151:8848"
  # 集群日后改为: "10.0.0.1:8848,10.0.0.2:8848,10.0.0.3:8848"
  # autosql；若误发在 public，改成 "" 或 public
  namespace: "e4dec2e2-4711-432f-9110-cc9b67d66b81"
  group: DEFAULT_GROUP
  data_id: auto-dba.yaml
  username: nacos
  password: "***"
```

`enabled: false` 时行为与改造前一致，只读本地 YAML。

环境变量（覆盖引导，便于容器）：

| 变量 | 含义 |
|------|------|
| `NACOS_ENABLED` | `true` / `1` 开启 |
| `NACOS_ADDR` | 支持逗号多地址 |
| `NACOS_NAMESPACE` | Namespace ID |
| `NACOS_GROUP` / `NACOS_DATA_ID` | Group / DataId |
| `NACOS_USERNAME` / `NACOS_PASSWORD` | 登录 |

合并顺序：代码默认 → 本地 YAML → Nacos DataId → 既有少量 env（如 `AUTO_DBA_DB_MONITOR_*`）。

## 在控制台发布 DataId

1. 登录后左侧切到 Namespace `e4dec2e2-4711-432f-9110-cc9b67d66b81`
2. 配置管理 → 配置列表 → 新建：
   - Data ID：`auto-dba.yaml`
   - Group：`DEFAULT_GROUP`
   - 格式：YAML
3. 粘贴正文（至少含密钥项）并**发布**

示例正文：

```yaml
app:
  jwt_secret: "<至少 32 位随机串>"
  jwt_ttl_hours: 24
db:
  driver: postgres
  dsn: "postgres://USER:PASSWORD@HOST:5432/db_autosql?sslmode=disable"

# 服务监听（启动时 Load 合并后再 Listen；改端口后需重启）
http:
  host: "0.0.0.0"
  port: 8800

terminal:
  enabled: true
  ssh_user: sysadmin
  ssh_private_key: |
    -----BEGIN OPENSSH PRIVATE KEY-----
    ...完整 PEM...
    -----END OPENSSH PRIVATE KEY-----
  ssh_host_key_policy: accept-new
```

不要在 DataId 里再写 `nacos:` 段。

### 服务端口从 Nacos 绑定

启动顺序已经是：**读本地引导 → 拉 Nacos 合并 → `Listen(host:port)`**，无需再写一套绑定逻辑。

1. 在 DataId `auto-dba.yaml` 增加或修改：

```yaml
http:
  host: "0.0.0.0"   # 监听地址；开发代理会把 0.0.0.0 改写成 127.0.0.1
  port: 8800        # 主 API；Nacos 非空 port 覆盖本地 config.yaml

# 前端开发服务（Vite）；生产静态资源不走此端口
frontend:
  dev_host: "127.0.0.1"
  dev_port: 5174
```

2. 发布后**重启** `./bin/auto-dba`（一期不热更）。
3. 日志中应出现：`auto-dba listening on 0.0.0.0:8800`（端口以合并后的值为准）。
4. 独立监控进程用 `services.db_monitor.port`（可同样放进 Nacos）。
5. **前端开发**：`pnpm --dir frontend dev` 启动时执行 `go run ./scripts/print_dev_api_proxy`，与后端同一套 `config.Load` 读取 Nacos 的 `http.*`（API 代理）与 `frontend.dev_*`（Vite 监听），**无需在前端写死端口**。改端口后须同时重启后端与 Vite。

合并优先级：Nacos 里非空的 `http.port` **覆盖**本地；Nacos 未写的 `host` / 超时等保留本地值。

核对：

- 控制台切到 **autosql**，能看到已发布的 `auto-dba.yaml`
- 部分 Nacos 版本 HTTP OpenAPI（`/nacos/v1/cs/configs`）可能 404，以控制台或应用启动日志为准：
  `已从 Nacos 加载配置 dataId=auto-dba.yaml ...`
- 若仍可用 v1 API：

```bash
curl -u 'nacos:密码' \
  "http://10.100.113.151:8848/nacos/v1/cs/configs?dataId=auto-dba.yaml&group=DEFAULT_GROUP&tenant=e4dec2e2-4711-432f-9110-cc9b67d66b81"
```

`tenant` 空则打到 public，与本地填 autosql ID 时会对不上。

## SSH 私钥

- Nacos 字段：`terminal.ssh_private_key`（PEM 正文）
- 启动后写入 `config/terminal/.runtime_id_ed25519`（权限 `0600`），并设置 `ssh_identity_file` 指向该文件
- 现有终端 / Agent SSH 下发逻辑不变；`config/terminal/` 已 gitignore

## 行为说明

- 启动时 `GetConfig`；与 `ListenConfig` **共用同一 Nacos 客户端**（按 pid 隔离本地 cache）
- `nacos.watch` 默认开启；推送约 **800ms debounce** 后合并，避免连发发布多次 DSN 热切
- 热更写配置时持写锁；鉴权读 `config.CurrentJWT()`（atomic）
- `enabled: true` 且 Nacos 不可达或 DataId 为空：**拒绝启动**
- 日常 HTTP 请求**不**访问 Nacos（无持续 QPS 开销）

### 热更范围（按配置段统一）

DataId 内全部业务顶层段参与 diff。段内任意字段都会写入内存。

| 变更 | 真实行为 |
|------|----------|
| `jwt_secret` | atomic + 各 engine/handler 密钥拷贝；**旧 JWT 立即失效**；元库中用其加密的 **SSO Client Secret / LDAP Bind 密码**也会解不开，须到「认证配置」重新填写并保存（见 [功能说明 · SSO](features.md#sso)） |
| `db` 池参数 | `ApplyPool` |
| `db.dsn` | **先** `Reopen`（migrate/seed），成功后再提交配置；失败则**整次热更中止**，内存 DSN 保持旧值 |
| `terminal.*`（含 `max_lifetime_sec`、SSH） | 写入 cfg；同步 backup / srcluster / dbdeploy SSH；新终端会话用新 lifetime |
| `flashback` / `cdc` / `backup` | 同步对应 Runtime/Engine |
| `external` / `migration` 等 | 写入内存 cfg |
| `log.level` | 仅写入 cfg（标准库 log 无动态级别过滤） |
| `http.read/write_timeout`、`serve_static` | 写入 cfg；**已 Listen 的 Server 需重启**才改超时 |
| `app.run_mode` | 写入 cfg；Gin mode **需重启** |
| `http.host` / `http.port` | 日志提示；`restart_on_listen_change: true` 时 Exec 自重启 |
| `frontend.dev_*` | 写入 cfg；**须重启 Vite** |
| `services.db_monitor` | 写入 cfg；模式/端口 **须重启**相关进程 |

本地引导：

```yaml
nacos:
  # watch: true   # 默认开启；false 关闭热更
  restart_on_listen_change: false
```

### 前端开发代理

```bash
# 推荐：预编译后再 pnpm dev（避免每次 go run）
./scripts/build-dev-api-proxy.sh
pnpm --dir frontend dev

# 或跳过 Nacos，直接注入：
# AUTO_DBA_DEV_ENDPOINTS_JSON='{"api_target":"http://127.0.0.1:8804","vite_host":"127.0.0.1","vite_port":5174}' pnpm --dir frontend dev
```

## 启动

```bash
# 1. 复制引导（若尚未配置 nacos:）
#    参考 config/bootstrap.example.yaml → 写入 config/config.yaml 顶部

# 2. 编译并启动（敏感项已在 Nacos DataId）
go build -o bin/auto-dba ./cmd/auto-dba
./bin/auto-dba -config config/config.yaml start

# 监控进程同样 Load，共用 jwt_secret 与 db.dsn
go build -o bin/db-monitor ./cmd/db-monitor
./bin/db-monitor -config config/config.yaml start
```

联调清单：

- [ ] 控制台对应 Namespace 下已发布 `auto-dba.yaml`（含 `jwt_secret`、`db.dsn`；SSH 用则含 `ssh_private_key`）
- [ ] 本地 `nacos.enabled: true`，`namespace` 为**平台部署** Namespace ID（非业务环境 code）
- [ ] 启动日志有「已从 Nacos 加载配置」与 `nacos watch: listening ... debounce=`
- [ ] 若配置了私钥：存在 `config/terminal/.runtime_id_ed25519`（0600）
- [ ] 改 `jwt_secret` / 非端口字段后出现 `nacos watch: applied`；DSN 失败应看到 `before-apply aborted` 且旧库仍可用
- [ ] 改 `http.port` 有提示；`restart_on_listen_change: true` 时自重启
- [ ] 改 `frontend.dev_port` 后重启 Vite；优先使用 `bin/print_dev_api_proxy`
