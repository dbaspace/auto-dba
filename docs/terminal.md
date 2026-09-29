# 主机终端（Web SSH）

路径：`/ops/terminal`；实例列表也可对 **host** 类型点「打开终端」。平台通过 WebTTY 在浏览器里开 SSH，**只用密钥登录**，不用数据库账号密码。

## 平台管什么、不管什么

| 平台管 | 平台不管 |
|--------|----------|
| 登记 host 实例、鉴权、会话 JWT | 在目标机装 sshd / 建系统用户 |
| 用统一私钥发起 SSH（`ssh -i`） | 把私钥下发到业务机 |
| Web 页 + WebSocket 转发 PTY | 密码登录、跳板机二次跳转编排 |
| 会话超时、可见性（全局/私有授权） | 各主机 `authorized_keys` 内容运维（需人工或自动化一次性配好） |

一句话：平台侧持有**一把私钥**；每台要登录的主机上，对应 SSH 用户的 **`~/.ssh/authorized_keys` 必须含该私钥的公钥**。

## 密钥怎么放（最易错）

```
┌─────────────────────────┐         SSH publickey          ┌──────────────────────────┐
│  auto-dba 平台机          │  ──────────────────────────►  │  目标主机                  │
│  config/terminal/         │   用户: sysadmin（或实例填写）   │  /home/sysadmin/.ssh/      │
│    id_ed25519      私钥   │   端口: 实例 Port，默认 22      │    authorized_keys  ←公钥  │
│    id_ed25519.pub  公钥   │                               │  （不要放私钥）             │
└─────────────────────────┘                               └──────────────────────────┘
```

| 位置 | 应放 | 不应放 |
|------|------|--------|
| 平台 `config/terminal/id_ed25519` | **私钥**（权限建议 `600`） | — |
| 目标机 SSH 用户家目录 | **公钥**一行进 `authorized_keys` | 私钥文件、随意路径的 `~/xxx` 密钥文件 |

把公私钥只拷到目标机某个文件（如 `~/auto-dba-terminal`）**不会**生效；sshd 只认登录用户的 `authorized_keys`。

### 生成与分发示例

```bash
# 在平台仓库（勿提交私钥；config/terminal/ 已 gitignore）
ssh-keygen -t ed25519 -f config/terminal/id_ed25519 -C auto-dba-terminal -N ''

# 查看公钥（分发用）
cat config/terminal/id_ed25519.pub
```

在**目标机**（以用户 `sysadmin` 为例，可用 root 代建）：

```bash
mkdir -p /home/sysadmin/.ssh
chmod 700 /home/sysadmin/.ssh
# 将平台 id_ed25519.pub 的整行追加进去
echo 'ssh-ed25519 AAAA... auto-dba-terminal' >> /home/sysadmin/.ssh/authorized_keys
chmod 600 /home/sysadmin/.ssh/authorized_keys
chown -R sysadmin:sysadmin /home/sysadmin/.ssh
```

平台机自测（通过后再用 Web 终端）：

```bash
ssh -i config/terminal/id_ed25519 -p 22 sysadmin@<主机IP>
```

## 配置

`config/config.yaml`（参考 `config/config.example.yaml`）：

```yaml
terminal:
  enabled: true
  ssh_user: sysadmin              # 实例未填 Username 时回退
  ssh_identity_file: config/terminal/id_ed25519
  ssh_host_key_policy: accept-new # accept-new | strict | insecure
  ssh_known_hosts_file: ""        # 空则默认 config/terminal/known_hosts
  scripts_dir: ""                 # 空则默认 scripts/terminal
  static_dir: ""                  # 空则默认 assets/terminal
  max_lifetime_sec: 1800          # 会话最长秒数
```

| 项 | 说明 |
|----|------|
| `enabled` | `false` 时打开终端接口直接拒绝 |
| `ssh_identity_file` | 平台私钥路径；文件不存在会创建会话失败 |
| `ssh_user` | 默认 SSH 用户，默认回退为 `sysadmin` |
| `scripts_dir` | 含 `node-terminal.sh`：实际执行 `ssh -tt -i <key> user@host` |
| `max_lifetime_sec` | JWT / 会话生命周期 |

Backup Agent **SSH 下发**复用同一套 `terminal.ssh_identity_file` / `ssh_user`，密钥与终端共用。

### Agent SSH 下发的主机密钥校验

Platform Agent（:8090）、Node Agent（:8091）、DB Agent（:8092）的 SSH 下发/启停/预检都走平台内置 Go SSH 客户端（`internal/sshx`），按 `ssh_host_key_policy` 校验目标机指纹：

| 策略 | 行为 |
|------|------|
| `accept-new`（默认） | 未登记的主机：记录指纹到 `ssh_known_hosts_file` 并放行；已登记且一致：放行；**指纹变化：拒绝** |
| `strict` | 只接受已登记的主机，需先 `ssh-keyscan -p <端口> <IP> >> config/terminal/known_hosts` |
| `insecure` | 不校验（仅开发）；旧配置 `ssh_insecure_host_key: true` 等同此策略 |

- 指纹文件默认 `config/terminal/known_hosts`（目录 0700、文件 0600，自动创建），**不读取**运行用户的 `~/.ssh/known_hosts`；容器部署需把 `config/terminal/` 挂持久卷，否则重建后会重新首次信任。
- 目标机重装系统 / 更换 IP 复用导致指纹变化时，下发会报「SSH 指纹与 known_hosts 记录不一致」，确认无误后执行提示中的 `ssh-keygen -R <IP> -f <known_hosts 路径>` 删除旧记录再重试。
- Web 终端走系统 `ssh`（`node-terminal.sh`，`StrictHostKeyChecking=no`），不受此配置影响。

## 登记主机实例

「配置 → 实例管理」新建，类型选 **host**（或 `os` / `主机`）：

| 字段 | 用途 |
|------|------|
| Host / Domain | SSH 目标地址（优先 Host） |
| 端口 | SSH 端口（页面上对 host 显示为 SSH 端口），默认 22 |
| 账号 / Username | SSH 用户；空则用 `terminal.ssh_user` |
| 可见性 | `global` 或私有 + 授权；不可见则无法打开终端 |

**密码字段对终端无效**（密钥登录）。实例密码可留空。

需具备菜单权限 `um.sidebar.ops-terminal`（主机终端），以及对该实例的可见性。

## 使用流程

1. 配好平台私钥 + 目标机 `authorized_keys`（见上）。
2. 登记 host 实例并授权。
3. 打开 `/ops/terminal`（或实例列表「打开终端」）→ 选择主机 → **打开终端**。
4. 前端 `POST /api/v1/instances/:id/open-terminal` → 返回 `terminal_url`（带短时 JWT）→ 新窗口加载 WebTTY。
5. 浏览器连 `GET /api/v1/terminal/ws?token=…`；平台进程执行 `scripts/terminal/node-terminal.sh <host> <user> <key> <port>`，把 PTY 接到 WebSocket。

### 会话状态（简要）

`created` → `running` → `finished` / `failed` / `expired`（超时或关闭）。

## 相关 API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/instances/:id/open-terminal` | 推荐；按实例鉴权后发 token |
| POST | `/api/v1/terminal` | 兼容：`action_name=node.terminal`，`args.host` 须已登记 |
| GET | `/api/v1/terminal?token=` | Web 页 |
| GET | `/api/v1/terminal/ws?token=` | WebSocket（子协议 `webtty`） |

## 排障

| 现象 | 常见原因 | 处理 |
|------|----------|------|
| `Permission denied (publickey,…)` | 公钥未进**登录用户**的 `authorized_keys`；或用户/端口不对 | 按上文写入 `sysadmin`（或实例 Username）的 authorized_keys；平台用同私钥 `ssh -i` 自测 |
| `SSH 私钥不存在` | `ssh_identity_file` 路径错或文件未挂进容器 | 检查路径与权限；容器需挂载 `config/terminal/` |
| `仅支持主机类型实例` | 实例 `db_type` 不是 host | 改类型或另建 host 实例 |
| `未授权访问该主机终端` | 可见性/授权不足 | 调 visibility 或授权 |
| `终端功能未启用` | `terminal.enabled: false` | 改为 `true` 并重启 |
| OpenSSH 提示 *post-quantum key exchange* | 客户端告警，**不是**登录失败原因 | 可忽略；失败看 `Permission denied` 等实质错误 |
| known_hosts / Permanently added | 脚本使用 `StrictHostKeyChecking=no` | 正常；不表示鉴权成功 |
| Agent 下发 `knownhosts: key is unknown` | 旧版本严格校验 `~/.ssh/known_hosts` | 升级后默认 `accept-new` 自动登记；或设 `ssh_host_key_policy: strict` 并先 `ssh-keyscan` |
| Agent 下发「SSH 指纹与 known_hosts 记录不一致」 | 目标机重装或 IP 被其他机器复用 | 核实后按提示 `ssh-keygen -R <IP> -f config/terminal/known_hosts` |

目标机侧还可核对：`PubkeyAuthentication yes`、家目录与 `.ssh` 权限过宽会被 sshd 拒绝、SELinux、防火墙放行 22（或自定义端口）。

## 安全注意

- `config/terminal/` 与 `config/config.yaml` **不要入库**；私钥泄露应立即轮换（新钥对 → 更新平台文件 → 更新各主机 authorized_keys → 作废旧钥）。
- **不要**把私钥拷到业务机「备用」；业务机只保留公钥。
- 终端会话有 `max_lifetime_sec`；生产建议配合 VPN / 跳板网络，避免公网直暴露平台与 SSH。
- 聊天、工单、截图中勿粘贴私钥全文。

## 代码落点

| 项 | 路径 |
|----|------|
| Handler / 会话 | `internal/terminal/` |
| SSH 脚本 | `scripts/terminal/node-terminal.sh` |
| 静态页 | `assets/terminal/` |
| 前端 | `frontend/src/views/ops/Terminal.tsx`；实例页打开入口 |
| 配置 | `config.Terminal` → `internal/config/config.go` |
| Agent 下发复用密钥 | `internal/backup/deploy.go` + `internal/sshx` |

---

**结论**：先保证「平台私钥 ↔ 目标机对应用户 authorized_keys」打通，再登记 host 实例从页面打开；Web 终端只是把这条 SSH 会话嵌进浏览器。
