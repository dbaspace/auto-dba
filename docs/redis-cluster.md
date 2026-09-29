# 集群部署 · Redis / Codis

侧栏：**平台运维 → 集群部署** → Hub `/ops/cluster-deploy` → `/ops/cluster-deploy/redis`。  
API：`/api/v1/db-deploy/*`（`engine=redis|codis`）。

与 MySQL/PG 共用工作台骨架；顶部**拓扑筛选**（自建单机 / 主从 / Sentinel / Codis / **云托管·单入口**）。Codis 用角色卡片（dashboard / proxy / server）。

## 拓扑

| 类型 | deploy_kind | 说明 |
|------|-------------|------|
| `standalone` | self | 自建单机 Redis |
| `master_replica` | self | 自建主从；有从须复制验收 |
| `sentinel` | self | Redis + Sentinel；可选绑定 Sentinel 控制器 |
| `codis` | self | **仅自建** Codis 组件编排 |
| `cloud_endpoint` | cloud | **云 Redis 单入口**（与云 MySQL/PG 相同；厂商 HA 仍是一个 endpoint） |

**云上没有 Codis**：登记云入口时 `engine` 强制为 `redis`，不可选 Codis。

版本包（自建）走**配置中心 OSS 预置** + Release 目录。

工作台支持 **新建集群**（仅自建 draft → 部署）与 **登记已有**（自建或云；云直接 ready，不装机）。

自建部署走变更单；主从与 Sentinel 必须通过 `repl/verify`。云托管不启停、不装机。

详见 [db-cluster.md](db-cluster.md) 共性约定。
