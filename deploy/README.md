# 数据库监控服务拆分

## 进程

| 进程 | 入口 | 默认端口 | 说明 |
|------|------|----------|------|
| 主 API | `go run ./cmd/auto-dba` | 8800 | 工单/配置/前端 API |
| 监控 API | `go run ./cmd/db-monitor` | 8801 | Prometheus 看板查询 |

两者**共用**：

- `app.jwt_secret`（同一 JWT，互相鉴权）
- `db.dsn`（同一元数据库：实例、标签、全局 Prometheus 配置）

前端仍请求主服务 `/api/v1/db-monitor/*`；`mode=remote` 时主服务反向代理到监控 Pod。

## 配置

```yaml
services:
  db_monitor:
    mode: embed          # embed | remote | off
    url: ""              # remote 时：http://auto-dba-db-monitor:8801
    host: "0.0.0.0"
    port: 8801
```

环境变量（便于 K8s）：

- `AUTO_DBA_DB_MONITOR_MODE=remote`
- `AUTO_DBA_DB_MONITOR_URL=http://auto-dba-db-monitor:8801`
- `AUTO_DBA_DB_MONITOR_PORT=8801`

## 本地拆分启动

```bash
# 终端 1：监控服务
go run ./cmd/db-monitor -config config/config.yaml start

# 终端 2：主服务（代理到监控）
AUTO_DBA_DB_MONITOR_MODE=remote \
AUTO_DBA_DB_MONITOR_URL=http://127.0.0.1:8801 \
  go run ./cmd/auto-dba -config config/config.yaml start
```

本地不想拆时保持 `mode: embed`（默认）即可，行为与拆分前一致。

## 镜像构建

```bash
# 主 API
docker build -f deploy/Dockerfile --build-arg APP=auto-dba -t auto-dba:latest .

# 监控服务
docker build -f deploy/Dockerfile --build-arg APP=db-monitor -t auto-dba-db-monitor:latest .
```

## K8s

见 [k8s/db-monitor-split.yaml](k8s/db-monitor-split.yaml)。同 Namespace 内通过 Service DNS：

`http://auto-dba-db-monitor:8801` ↔ 主服务 `AUTO_DBA_DB_MONITOR_URL`。
