# 平台 Agent（Platform Agent）

部署在可直连数据库的跳板或同机房主机，供 auto-dba 编排。**定义入口在配置中心**，不属于备份/高可用等业务模块。

- **备份控制**：逻辑 / 物理备份（PG / MySQL）
- **Redis RDB**：`redis-cli --rdb` 拉取，供内存分析
- **高可用**：环境检查 + MHA 计划切换（`/v1/mha/*`，与独立 mha-agent 同路径）

产品名：**平台 Agent**。本目录源码仍位于 `deploy/backup-agent`（历史路径），制品名称为 `platform-agent`（兼容产出 `backup-agent`）。

## 平台侧入口

| 位置 | 说明 |
|------|------|
| **配置中心 → 平台 Agent**（`/config/agents`） | 登记 / SSH 下发 / 勾选**归属** |
| 数据备份 → 备份策略 | 引用归属含 `backup` 的 Agent |
| Redis 内存分析 | 引用归属含 `rdb` 的 Agent |
| 高可用 → 控制器 | 可选用归属含 `ha` 的 Agent（kind=mha） |

## 构建 linux 制品

```bash
cd deploy/backup-agent
bash build.sh
# 产物：bin/platform-agent 与 bin/backup-agent（同内容）
```

配置（`config/config.example.yaml`）：

```yaml
backup:
  agent_artifact: deploy/backup-agent/bin/platform-agent
  agent_remote_dir: /home/sysadmin/auto-dba/platform-agent
  agent_listen_port: 8090
```

## 下发时勾选归属

「配置中心 → 平台 Agent」→ 下发到主机 → **归属**：

| 归属 | 预检必选工具 |
|------|--------------|
| 备份控制 + 实例类型 | 按所选类型：PostgreSQL→`pg_dump`；MySQL/TiDB/OceanBase→`mysqldump`（物理工具可选） |
| Redis RDB 分析 | `redis-cli` |
| 高可用 | `masterha_check_status`（计划切换另需 `masterha_master_switch`） |

勾选组内必选缺失 → **阻断下发**并提示安装方式；未勾选组不检查。

重新下发后旧 Agent 才会具备新接口。

## 可选提权（sudo su -）

SSH 仍以实例 Username（常见 `sysadmin`）+ 平台私钥登录。若目标机工具装在 root 环境、或目录需 root 写权限，下发表单可开启 **提权执行**：

| 项 | 说明 |
|----|------|
| 现场手工 | `sudo su -` 切到 root 再检查 |
| 平台非交互等价 | `sudo -n su - root -c '<command>'` |
| 适用范围 | 预检、下发 mkdir/chmod/启停；SFTP 上传前会 `chown` 给登录用户 |
| 前置 | 登录用户需 **NOPASSWD** sudo，否则预检报「提权探针失败」 |

sudoers 示例（收窄或按现场策略调整）：

```text
sysadmin ALL=(ALL) NOPASSWD: ALL
```

开启提权后远程目录可用 `/opt/auto-dba/platform-agent` 等；未开启时请继续用登录用户可写的家目录路径。

Agent tags 会写入 `escalate:sudo_su`，重新下发/启停自动复用。

## 契约

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/healthz` | 探活 |
| GET | `/v1/capabilities?groups=backup,rdb,ha` | 本机能力探测 |
| POST | `/v1/ha/env-check` | HA 环境检查（kind=mha） |
| GET | `/v1/mha/status?conf=` | `masterha_check_status` |
| GET | `/v1/mha/repl?conf=` | `masterha_check_repl` |
| POST | `/v1/mha/switch/planned` | 计划切主 |
| POST | `/v1/backup/logical` | 逻辑备份 |
| POST | `/v1/backup/physical` | 物理备份 |
| POST | `/v1/backup/redis-rdb` | 拉取 RDB（octet-stream） |
| GET | `/v1/backup/jobs/:id` | 任务查询 |

`-conf-root`（默认 `/etc/mha`）限制 MHA conf 路径白名单。

独立 [mha-agent](../mha-agent/) 仍可单独部署；与平台 Agent 同机时可用后者统一接入。

## 依赖工具

| 能力 | 工具 |
|------|------|
| 逻辑 PG | `pg_dump` |
| 逻辑 MySQL | `mysqldump` |
| 物理 PG | `pg_basebackup` |
| 物理 MySQL | `xtrabackup` / `mariabackup` |
| Redis RDB | `redis-cli` |
| HA(MHA) | `masterha_check_status` / `masterha_check_repl` / `masterha_master_switch` |
