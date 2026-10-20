# 能力缺口补齐实施记录

对应计划：平台能力缺口分析（Cursor plan）与本文。

## 已落地（代码）

| 优先级 | 项 | 状态 | 说明 |
|--------|----|------|------|
| P0 | MySQL 族 DDL/DML 自动执行 | **已完成** | `ticketexec.Executable` 含 mysql/tidb/oceanbase |
| P0 | 逻辑备份恢复工单 | **已完成** | `POST /backup/restore` → Agent `/v1/restore/logical` |
| P0 | 物理全量恢复 | **已完成** | `restore_kind=physical` + Agent `/v1/restore/physical` |
| P0 | 物理增量链恢复 | **已完成** | 沿 `parent_run_id` 拼 base→tip；Agent xtrabackup `--incremental-dir`（MySQL） |
| P0 | PITR（时间点恢复骨架） | **已完成** | `restore_kind=pitr` + Agent `/v1/restore/pitr` |
| P0 | CDC MySQL 源路径 | **基础完成** | MySqlConnector + 编排预检 + 前端源引擎选择 |
| P0 | CDC MySQL server.id 冲突 | **已完成** | 与 `@@server_id` 及同源活跃任务去重 |
| P1 | HA ForcedFailover 对称 | **已完成** | ProxySQL / repmgr / Sentinel |
| P1 | MySQL 会话 | **已完成** | processlist + KILL |
| P1 | MySQL 慢 SQL digest | **已完成** | `/ops/mysql-slow` |
| P1 | 结构对比列/索引 | **已完成** | 列 MODIFY；索引 DROP+ADD |
| P1 | 闪回多云 WAL | **骨架完成** | tencent / aliyun / huawei / aws |
| P1 | K8s Helm 列表 | **增强** | revision 去重、chart/updated |
| P1 | Helm install/upgrade/rollback | **CLI 骨架** | 本机 `helm` + 临时 kubeconfig；非完整 Go SDK |
| — | PITR 与策略联动连续归档入库 | **未做** | — |
| — | 闪回华为/AWS 官方 SDK | **未做** | 现为 HTTP stub |

## 使用说明

### 备份恢复

路径：`/ops/backup/restore`

- **逻辑** / **物理全量** / **PITR**：见既有说明。
- **物理增量**：选择 `physical_incr` 成功 run 提交物理恢复即可；平台自动回溯 `parent_run_id` 得到 base→tip 目录链，Agent 对 MySQL 执行 xtrabackup 增量 prepare + copy-back。PostgreSQL 增量请用 PITR（WAL）。

### CDC MySQL

创建同步工单时可选源引擎 **PostgreSQL / MySQL**；MySQL 须配置不冲突的 Debezium `database.server.id`（`mysql_server_id`）。

### Helm Day2

路径：容器 → Helm 应用。

- 列表：Secret 只读汇总。
- `POST /k8s/clusters/:id/apps/upgrade`：`helm upgrade --install`（平台主机需 helm）。
- `POST /k8s/clusters/:id/apps/rollback`：`helm rollback`。
- Agent 隧道接入可能无法写临时 kubeconfig 直连 API；优先 kubeconfig/token 直连集群。

## 建议下一迭代

1. PITR：策略级 WAL/binlog 连续归档入库
2. 闪回华为/AWS：官方 SDK 下载鉴权
3. Helm：嵌入 helm.sh/helm/v3 Go SDK（摆脱本机 CLI）
4. CDC MySQL：分库/分表向导与 PG 能力对齐细化

## 关键代码路径

- `internal/backup/restore.go`（`resolvePhysicalRestoreChain`）、`deploy/backup-agent/restore.go`
- `internal/cdc/orchestrate.go`（server.id 冲突）、`frontend/.../CdcCreate.tsx`
- `internal/k8s/helm_cli.go`、`internal/handler/k8s_db.go`、`frontend/.../k8s/Apps.tsx`
