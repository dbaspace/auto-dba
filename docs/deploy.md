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
| **一体**（默认） | `./scripts/pack_release.sh` 或 `… all` | `dist/auto-dba-release-linux-amd64-*.tgz` | CentOS 手工部署，一进程托管页面+API |
| **仅后端** | `./scripts/pack_release.sh backend` | `dist/auto-dba-backend-linux-amd64-*.tgz` | 只要 API；前端仍用开发 Vite 或另外部署 |
| **仅前端** | `./scripts/pack_release.sh frontend` | `dist/auto-dba-frontend-*.tgz` | Nginx/CDN 托管静态资源 |
| **Docker** | `./scripts/pack_release.sh docker` | 镜像 `auto-dba:<时间戳>`（可用 `IMAGE_TAG=` 覆盖） | 容器环境 |

可选环境变量：`GOOS` `GOARCH`（默认 `linux`/`amd64`）、`STAMP`（默认时间戳；发版用 `STAMP=v1.3.0`）、`IMAGE_TAG`、`SKIP_PNPM_INSTALL=1`（已有 `node_modules` 时）。

```bash
STAMP=v1.3.0 ./scripts/pack_release.sh all
STAMP=v1.3.0 ./scripts/pack_release.sh backend
STAMP=v1.3.0 ./scripts/pack_release.sh frontend
IMAGE_TAG=auto-dba:v1.3.0 ./scripts/pack_release.sh docker
```

### 已发布版本（摘要）

| 版本 | 要点 |
|------|------|
| [v1.1.0](https://github.com/dbaspace/auto-dba/releases/tag/v1.1.0) | 一体/分服务打包；SPA `serve_static` 白屏硬化；部署文档 |
| [v1.2.0](https://github.com/dbaspace/auto-dba/releases/tag/v1.2.0) | 租户删除（无成员且无绑定资源）；租户管理列表一屏布局；`um.action.tenants.delete` |
| [v1.3.0](https://github.com/dbaspace/auto-dba/releases/tag/v1.3.0) | 平台管理员创建/切换业务租户时保留默认租户成员；顶栏稳定显示租户「名称」；删除当前会话租户后自动切回可用租户 |

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

包内结构：

```text
bin/auto-dba
frontend/dist/
config/          # 示例 YAML
README-RELEASE.txt
```

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

镜像内已含 `frontend/dist`；请保证配置中 `http.serve_static: true`（Dockerfile 会在示例 config 末尾追加提示）。密钥、DSN 建议走 Nacos 或挂载配置，勿打进镜像。

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
