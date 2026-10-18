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

一体包与后端包解压后工作目录须为**包根**（存在 `deploy/node-agent/bin/node-agent` 等），集群部署 SSH 下发无需再本机构建 Agent。

可选环境变量：`GOOS` `GOARCH`（默认 `linux`/`amd64`）、`STAMP`（默认时间戳；发版用 `STAMP=v1.12.12`）、`IMAGE_TAG`、`SKIP_PNPM_INSTALL=1`（已有 `node_modules` 时）。

```bash
STAMP=v1.12.12 ./scripts/pack_release.sh all
STAMP=v1.12.12 ./scripts/pack_release.sh backend
STAMP=v1.12.12 ./scripts/pack_release.sh frontend
IMAGE_TAG=auto-dba:v1.12.12 ./scripts/pack_release.sh docker
```

## 发布到 GitHub Release（固定流程）

用 [`scripts/publish_github_release.sh`](../scripts/publish_github_release.sh)，**不要**依赖交互式 `gh auth login`（浏览器 OAuth 易超时）。

凭据来源与 `git push https://github.com/...` 相同：从本机 **git credential / Keychain** 取出 token，临时注入 `GH_TOKEN` 再调 `gh`（与 v1.11.0 / v1.12.0 发版成功路径一致）。

```bash
# 已 push 的提交 + 写好 docs/changelog-vX.Y.Z.md 后：

# 推荐：打包 + 打 tag + 创建 Release（一体包）
PACK=1 ./scripts/publish_github_release.sh v1.12.12

# 已有 dist/*-v1.12.12.tgz 时只发版
./scripts/publish_github_release.sh v1.12.12

# 发版前顺带删若干旧 Release（不删受仓库 Rules 保护的 tag）
DELETE_RELEASES='v2.3.6 v2.3.7' PACK=1 ./scripts/publish_github_release.sh v1.12.12
```

前提：本机能 `git push origin HEAD`；已安装 `gh`。若 Keychain 无凭据，先手动 push 一次或导出 `GH_TOKEN=...`。

### 已发布版本（摘要）

| 版本 | 要点 |
|------|------|
| [v1.12.12](changelog-v1.12.12.md) | **当前推荐**。告警通知补 host/目录；历史按通知次数 + 恢复行 |
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
