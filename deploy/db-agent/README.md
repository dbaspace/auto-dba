# Db Agent

部署在 MySQL / PostgreSQL / Redis 主机，提供 HTTP 白名单 API，供 auto-dba「集群部署」工作台编排装包/启停。

## 构建

```bash
./build.sh
# 产出 bin/db-agent（linux）
```

## 运行

```bash
./db-agent -listen :8092 -user agent -pass '***'
# 本地联调可加 -dry-run
```

## API

| 路径 | 说明 |
|------|------|
| `GET /healthz` | 探活 |
| `POST /v1/db/precheck` | 预检 |
| `POST /v1/db/fs/ensure` | mkdir |
| `POST /v1/db/file/put` | 原子写文件 |
| `POST /v1/db/file/get` | 读文件（限路径） |
| `POST /v1/db/artifact/install` | 预签名 URL 下载 + sha256 + tar（异步 job） |
| `POST /v1/db/systemd/install` | 写 unit |
| `POST /v1/db/systemd/ctrl` | start/stop/restart/… |
| `POST /v1/db/exec` | 白名单：`repl_setup`（mysql/redis 复制建立） |
| `POST /v1/db/repl/verify` | 复制/Sentinel 验收 |
| `GET /v1/db/jobs/:id` | 异步任务 |

制品须预置于配置中心 OSS，由控制面签发 download_url。工作台支持 **SSH 下发** 本 Agent（默认 `:8092`）到 host 实例。详见 `docs/db-cluster.md`。

与 StarRocks **Node Agent**（`:8091`）及配置中心**平台 Agent**（`:8090`）相互独立，勿混用。
