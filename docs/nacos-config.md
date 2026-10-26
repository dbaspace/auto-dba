# Nacos 核心配置

KeePlus 启动时可选从 Nacos 拉取业务配置（元库 DSN、`jwt_secret`、SSH 私钥等），本地文件只保留**如何连接** Nacos。  
后端进程 `auto-dba` 与 `db-monitor` 共用 `config.Load`，同一 DataId（工程约定名 `auto-dba.yaml`，与产品品牌 KeePlus 无关）。

引导示例：[`config/bootstrap.example.yaml`](../config/bootstrap.example.yaml)、[`config/config.example.yaml`](../config/config.example.yaml)。仓库总览入口见 [README](../README.md#能力概览)。生产打包与 `http.serve_static` 见 [deploy.md](deploy.md)。

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
4. **引导默认不写 `namespace`（= public）**。仅当 DataId 建在其它空间时，再写该空间的 **Namespace ID**（不要填显示名），或设环境变量 `NACOS_NAMESPACE`

## 当前环境（单机）

| 项 | 值 |
|----|-----|
| 地址 | `10.100.113.151:8848`（控制台 / HTTP；gRPC 另用 9848） |
| 默认 Namespace | **public**（引导文件省略 `namespace` 即可） |
| 其它空间示例 | `autosql` → ID `e4dec2e2-4711-432f-9110-cc9b67d66b81`（仅当 DataId 在该空间时才写入引导） |
| Group | `DEFAULT_GROUP` |
| Data ID | `auto-dba.yaml`（或按部署改 `prod-auto-dba.yaml` 等） |

控制台：`http://10.100.113.151:8848/nacos`

### `nacos.namespace` 怎么填

**默认：配置文件里不要写 `namespace` 键**（反序列化为空 = public）。只改 `data_id` 时不会被错误的 Namespace ID 带跑。

客户端只认 **Namespace ID**，与控制台「命名空间」表第二列一致；须与 DataId **实际所在空间**一致：

| 控制台名称 | 本地写法 |
|------------|----------|
| **public**（默认） | **省略**该键；或 `""` / 字面量 `public`（代码当成空串） |
| **autosql** 等 | 显式 `namespace: "e4dec2e2-4711-432f-9110-cc9b67d66b81"`，或 `NACOS_NAMESPACE` |

若引导写了 autosql ID、而 DataId 只在 public 发布，启动会 `config data not exist`。控制台左上角先切到对应空间再编辑/核对 DataId。

## 业务环境 vs Nacos Namespace（方案 A）

平台里有两套容易混淆的「环境」，**不要混用**：

| 概念 | 存在哪里 | 作用 |
|------|----------|------|
| **业务环境**（配置中心 → 环境管理：`test` / `beijing` / `ai-beijing` 等） | 元库 `tbl_environments` | 实例 / 区域 / 工单的标签与筛选；**一套 auto-dba 同时服务多个** |
| **平台部署配置** | Nacos **Namespace** + 本地 `nacos.namespace` | 本进程的 `jwt_secret` / `db.dsn` / `http.port` 等；**一套进程只连一个 Namespace** |

推荐用法：

1. 每个**平台部署**（开发 / 预发 / 生产）建一个 Nacos Namespace（显示名如 `auto-dba-dev`、`autosql`），记下 **Namespace ID**。
2. 各 Namespace 下维护同名 DataId `auto-dba.yaml`（Group `DEFAULT_GROUP`），内容按该部署分别填 DSN、JWT、端口。
3. DataId 在 **public** 时引导**省略** `namespace`；若在独立 Namespace，再写该空间的 **Namespace ID**（或 `NACOS_NAMESPACE`），不要填业务环境 `code`。
4. 业务环境继续只在「环境管理」维护；与 Nacos、热更、Vite 代理无关。

反例：

- 不要为 `beijing`、`test`、`frankfurt` 等业务 code 各建一个 Nacos Namespace（与「单进程多业务环境」模型冲突，密钥/DSN 也会被错误拆散）。
- 不要把环境管理里的编码写进 `nacos.namespace`。
- 不要在引导里写上 Namespace ID，却把 DataId 发在另一个空间（例如写了 autosql ID、配置却在 public）。

元库里的 `test` / `beijing` 等只是业务数据标签，与 Nacos Namespace 无关。

## 本地引导

`config/config.yaml` 顶部（参考 `config/config.example.yaml`）：

```yaml
nacos:
  enabled: true
  server_addr: "10.100.113.151:8848"
  # 集群日后改为: "10.0.0.1:8848,10.0.0.2:8848,10.0.0.3:8848"
  # 默认不写 namespace（= public）。DataId 在其它空间时再加，例如：
  # namespace: "e4dec2e2-4711-432f-9110-cc9b67d66b81"
  group: DEFAULT_GROUP
  data_id: auto-dba.yaml
  username: nacos
  password: "***"
```

字段加密密钥写在 **Nacos DataId**（或本地 `app:`）的 `crypto_secret`，不要写进本引导文件。

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
  jwt_secret: "<至少 32 位随机串>"          # 仅签 JWT（可轮换）
  jwt_ttl_hours: 24
  crypto_secret: "<另一段至少 32 位随机串>" # 实例密码等字段加密；长期固定，勿与 jwt 共用/同改
  # crypto_secret_previous: "<旧 crypto_secret 或旧 jwt>"  # 轮换窗口；缺则旧密文解不开
  # crypto_key_id: k1
db:
  driver: postgres
  dsn: "postgres://USER:PASSWORD@HOST:5432/db_autosql?sslmode=disable"

# 平台登录会话 Redis（与实例 Redis 工单无关；默认可不配=关闭）
# 启用后：登录按用户维度写入 Hash user:{uid}；管理端「登录会话」以 Redis 为准。
# 同一账号同时只保留最新一次登录：新登录会清空该用户旧会话，旧 token 下次请求返回会话失效。
redis:
  enabled: true
  addr: "10.x.x.x:6379"
  password: "***"            # 敏感，仅 Nacos
  db: 0
  key_prefix: "autodba:"
  session_ttl_hours: 24      # 空则回退 app.jwt_ttl_hours
  sliding_expire: false      # 已忽略：有效期内再次访问会把过期时间续到现在 + session_ttl；到期后提示重新登录
  fail_mode: closed          # closed=有 jti 时必须 Redis 命中；open=仅验 JWT
  max_sessions_per_user: 1   # 已固定为同一账号只保留最新登录（配置项保留兼容，运行时始终踢旧）
  reconcile_interval_min: 5

# 服务监听（启动时 Load 合并后再 Listen；改端口后需重启）
http:
  host: "0.0.0.0"
  port: 8800

terminal:
  enabled: true
  ssh_user: sysadmin
  # 必须用 | 块标量；私钥每一行都要缩进（勿顶格），否则报 expected ':'
  ssh_private_key: |
    -----BEGIN RSA PRIVATE KEY-----
    MIIE…（完整 Base64，每行与首行同缩进）
    -----END RSA PRIVATE KEY-----
  # 生产若只给裸 Base64（无 BEGIN/END、前缀常为 MIIE…），同样用 | 并缩进即可，启动/热更时会自动包成 PEM
  ssh_host_key_policy: accept-new

# DBA Copilot（核心密钥；仅从此段读取，不回退库表全局参数）
# Cursor 与 OpenAI 兼容网关不要混用同一段 base_url（通义/qwen 须走 openai 网关，勿挂 api.cursor.com）
copilot:
  base_url: "https://api.cursor.com"
  api_key: "crsr_…"                 # Dashboard → User API Keys；401 Invalid User API Key 时重新生成
  model: "auto"
  provider: "cursor"
  # —— 若要用「通义」等兼容模型，整段改成下面（勿与 Cursor 混用）——
  # base_url: "https://你的兼容网关/v1"   # 勿带 /chat/completions
  # api_key: "sk-…"
  # model: '[{"value":"qwen-plus","label":"通义"},{"value":"gpt-4o-mini","label":"轻量"}]'
  # provider: "openai"
  # JSON 列表请用单引号或 | 块，勿用 "[{"value":...}]"（内层双引号会弄断 YAML）
```

不要在 DataId 里再写 `nacos:` 段。

### 工单申请环境档

`ticket.env_groups` 只给工单申请页把环境收成「生产 / 测试」，并据此过滤数据源。实例登记、环境管理、域名上的 `env_code` 仍是 `beijing`、`test` 这类具体编码。热更后下一请求生效，不用重启。

```yaml
ticket:
  env_groups:
    - code: prod
      label: 生产
      codes: [beijing, pobeice, ai-beijing, ai-sea, frankfurt, siliconvalley, singapore]
    - code: test
      label: 测试
      codes: [test, dev, uat]
```

未写入任何一组的编码在申请页归到「其他」。省略整段时，编码含 `test` / `dev` 或等于 `uat` 归测试，其余归生产。SQL 检测和审核人仍按选中的数据源，不按这一档整组执行。

**Copilot 401 排查**：`Invalid User API Key` 表示上游拒绝密钥——检查 Nacos `copilot.api_key` 是否为有效 Cursor User API Key（`crsr_…`），或改用 OpenAI 兼容网关的 `sk-…`；前端选「通义」时必须配置 `provider: openai` 与对应 `base_url`，不能仍指向 `api.cursor.com`。

### 通知渠道（飞书 / 企微 / 短信 / 电话 / 群机器人）

邮件 SMTP 仍走库表「邮件」配置与 `email_on_*` 开关，**不在本段配置**。

**页面不再录入密钥 / Webhook URL / 电话 API**：企微 CorpID·Secret、群机器人 URL、短信/电话凭证等**仅写在本 `notify` 段**。认证配置 → 通知 Tab 按渠道分块，只提供事件开关与「发送测试」（`POST /settings/notify/test`）。默认全部 `enabled: false`，按使用再开。

```yaml
notify:
  provider:
    wechat:
      enabled: false
      corp_id: ""
      agent_id: ""
      api_secret: ""          # 勿写入仓库 / 聊天
      api_url: "https://qyapi.weixin.qq.com/cgi-bin/"
      # 告警默认发往这些部门（toparty）；部门须在应用「可见范围」内。支持 "15" 或 ["15","16"]
      party_ids: "15"
    lark:                    # 飞书私聊（工单审批人）
      enabled: false
      app_id: "cli_xxx"
      app_secret: "xxx"      # 泄露后须在开放平台轮换
    alert_lark:              # 飞书告警独立应用
      enabled: false
      app_id: ""
      app_secret: ""
    call: "cloopen"          # cloopen | huawei | yjzy
    sms: "cloopen"           # cloopen | yjzy
  tele_provider:
    cloopen:
      enabled: false
      api_account: ""
      api_token: ""
      app_id: ""
      template_id: ""
      singer: "【KeePlus】"
    cloopen_call:
      enabled: false
      api_account: ""
      api_token: ""
      app_id: ""
      display_num: ""
      media_name: ""
      tts: true
    huawei:
      enabled: false
      base_url: "https://rtccall.cn-north-1.myhuaweicloud.cn:443"
      api_key: ""
      api_secret: ""
      display_numbers: []
      template_id: ""
      status_url: ""
    yjzy_call:
      base_url: ""
      cid: ""
      pwd: ""
      product_id: ""
    yjzy_sms:
      base_url: ""
      cid: ""
      pwd: ""
      product_id: ""
      singer: "【KeePlus】"
  robots:
    - kind: lark             # lark | wechat | dingtalk
      url: ""
```

- **群机器人方向（告警 / 工单）不在 Nacos**：只在「认证配置 → 通知 → 群机器人」按条选择并保存到库（`robot_routes` + `robots_on_ticket_create`）。未配置方向时默认仅告警。
- 热更：`notify` 段变更会重建 Provider（与 Copilot 相同 Listen 路径）。
- 用户绑定飞书：`im_type=lark`（或留空），`im_account` 可填飞书 **user_id**（企业内用户 ID，非登录名）、`open_id`（`ou_`）、`union_id`（`on_`）、群 `chat_id`（`oc_`）、邮箱或手机号；也可加显式前缀 `email:` / `mobile:` / `user_id:` / `open_id:`。短信/电话用用户档案 `phone`。
- 飞书接收人顺序：已在「用户管理 → 同步 Lark」同步过的用户用 `open_id` → `user_id` → 飞书邮箱，不再用企业邮箱；未同步的用户用飞书邮箱 → `im_account` → 用户档案 `email` → 用户档案 `phone`。某个身份被飞书判为无效（如 `99992360`）时自动尝试下一个，单人失败不影响其他接收人。
- 飞书私聊认名片上的「邮箱」，不认「企业邮箱」：企业邮箱只给 SMTP。企业邮箱在飞书查不到的用户，在编辑用户里填「飞书邮箱」后再同步。
- 「同步 Lark」只处理登录过本平台的用户（内置 admin 除外）。先按飞书邮箱、企业邮箱、手机号每 25 人一批调用 `batch_get_id`；没对上且有邮箱的，再从 `open.larksuite.com` 分页读通讯录（企业邮箱只在这个域名返回），在内存里按企业邮箱或名片邮箱比对，全部对上就停止，单次最多 200 页。被限流时等待后重试一次，仍失败则停止后续请求。不保存飞书全员，不新建账号。复用本段 `lark` 应用，无需新增配置。
- 飞书应用权限：`im:message`（发送消息）；手机号发送与点查需 `contact:user.id:readonly`；按企业邮箱补齐还需通讯录部门、用户基本信息、邮箱、企业邮箱、user_id 的读取权限，且应用可用范围包含要同步的用户。
- 开启「工单创建通知审批人」（`lark_on_ticket_create`）后：所有工单提交 / 转移时通知审核人（当前审核人、DBA 审核人、安全审核人；DQL 逐关通知）；**建库工单**执行成功并授权后，另向申请人私聊发送地址、库名、账号、密码（账号已存在时为「沿用原密码」）、权限与说明；其他工单不向申请人发送飞书结果。
- API `GET /settings/notify` 返回 `providers` 只读状态（`lark_ready` 等），不含密钥。

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

- Nacos 字段：`terminal.ssh_private_key`（PEM 正文，或**无头尾的裸 Base64**）
- 支持格式：`BEGIN OPENSSH PRIVATE KEY`、`BEGIN RSA PRIVATE KEY` / `BEGIN PRIVATE KEY`（PKCS#1/8）、以及对应裸 Base64（OpenSSH 正文以 `openssh-key-v1` 识别）
- 启动 / 热更时归一化为 PEM，写入 `config/terminal/.runtime_id_ed25519`（`0600`），并设置 `ssh_identity_file`
- 热更用 PEM 内容指纹判断变更；仅改私钥也会 `nacos watch: applied … sections=[terminal]`
- **带口令的加密私钥暂不支持**热更（请用无口令钥，或 `ssh-keygen -p -N ''` 去口令后再贴）
- `config/terminal/` 已 gitignore

### 写法与常见报错

| 现象 | 原因 | 处理 |
|------|------|------|
| `parse remote yaml: … could not find expected ':'` | 块标量内后续行**顶格**或未写 `\|` | 每一行与首行同缩进；必须有 `ssh_private_key: \|` |
| 内容推送但业务字段无变化 | 旧版不比较密钥指纹 | 升级后按指纹热更；或重启一次 |
| `无法识别私钥 DER … pkcs8`（旧版） | OpenSSH 正文被误当 PKCS#8 | 升级含 OpenSSH 魔数识别的版本；或贴完整 `BEGIN OPENSSH PRIVATE KEY` |
| OpenSSH 私钥无法解析 | 内容截断或带加密口令 | 核对粘贴完整；去掉口令后再发布 |
| 头与正文类型不符 | `BEGIN OPENSSH` 塞 RSA `MIIE…`，或相反 | 只贴裸 Base64 让程序按正文自动包装，或头尾与正文一致 |

OpenSSH PEM 示例：

```yaml
terminal:
  ssh_private_key: |
    -----BEGIN OPENSSH PRIVATE KEY-----
    b3BlbnNzaC1rZXktdjEAAAAA…（每行缩进）
    -----END OPENSSH PRIVATE KEY-----
```

RSA 裸 Base64 示例：

```yaml
terminal:
  ssh_private_key: |
    MIIEowIBAAKCAQEA…
    xlSS2R5DbEDhrEja…
    （后续行同样缩进，不要顶格）
```

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
| `jwt_secret` | 仅签 JWT（atomic）；**旧 token 立即失效**。字段加密请用独立的 `crypto_secret`（见下），**不要**用改 JWT 的方式轮换实例密码密钥 |
| `crypto_secret` / `crypto_secret_previous` / `crypto_key_id` | 实例密码、SSO/LDAP/SMTP 等字段 AES（`enc:v2`）；热更后刷新各 handler Secret。**生产务必固定 `crypto_secret`**，与 JWT 分离。轮换：先把旧值写入 `crypto_secret_previous`，再改 `crypto_secret`。未配置 `crypto_secret` 时回退 `jwt_secret`（兼容旧部署）；此时若热更只改 JWT，进程会把旧 FieldCrypto 自动并入 previous 环以便仍能解密。换密钥后若 previous 未保留，须重填实例密码，并到「认证配置」重存 SSO/LDAP Secret（见 [功能说明 · SSO](features.md#sso)） |
| `copilot.*` | 写入 cfg；下次 `LoadConfig` 即生效（Chat/Models 每次现读 Live）；**不读**库表 `copilot_*` |
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
| `redis.*` | 写入 cfg；`enabled/addr/password/db` 变更时重连会话 Redis；关闭后鉴权回退为纯 JWT（与改造前一致） |

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

- [ ] 控制台对应 Namespace 下已发布 `auto-dba.yaml`（含 `jwt_secret`、`db.dsn`；SSH 用则含 `ssh_private_key`；Copilot 用则含 `copilot.*`）
- [ ] 本地 `nacos.enabled: true`；`namespace` 省略（public）或与 DataId 所在空间 ID 一致（非业务环境 code）
- [ ] 启动日志有「已从 Nacos 加载配置」与 `nacos watch: listening ... debounce=`
- [ ] 若配置了私钥：存在 `config/terminal/.runtime_id_ed25519`（0600），文件含 `BEGIN … PRIVATE KEY`（裸 Base64 会被自动包装）
- [ ] 仅改 `ssh_private_key` 后出现 `nacos watch: applied` 且 `sections` 含 `terminal`（旧版可能误报「业务字段无变化」）
- [ ] 改 `jwt_secret` / 非端口字段后出现 `nacos watch: applied`；DSN 失败应看到 `before-apply aborted` 且旧库仍可用
- [ ] 改 `http.port` 有提示；`restart_on_listen_change: true` 时自重启
- [ ] 改 `frontend.dev_port` 后重启 Vite；优先使用 `bin/print_dev_api_proxy`
