# MHA Manager Agent

部署在 **MHA Manager 同机**，把平台 HTTP 调用转成白名单 `masterha_*` / 装包 / systemd 命令。

平台 Agent（`deploy/backup-agent`，归属含高可用）与本独立二进制 **API 路径对齐**。

## 平台怎么管 MHA

| 管 | 不管 |
|----|------|
| 版本制品下载 + rpm/deb 安装（本机与 SSH 到节点） | 平台进程内选主 / 无审批自动切主 |
| `masterha_manager` 的 **systemd 启停与 status** | VIP/DNS 脚本业务逻辑 |
| conf 下发、check_status / check_repl | 业务库私钥（用现场 ssh_user） |
| 审批后的 alive / dead `masterha_master_switch` | |

自动 failover 仍由现场 `masterha_manager` 执行；平台负责装包、守护启停、审批切换与审计。

## 启动

```bash
# 开发自测（不真跑 masterha / systemd / 装包）
go run . -listen :8088 -dry-run -conf-root /tmp/mha

# 生产示例
go build -o mha-agent .
./mha-agent -listen :8088 -user agent -pass '***' -conf-root /etc/mha
```

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/healthz` | 探活 |
| GET | `/v1/mha/status?conf=` | `masterha_check_status` |
| GET | `/v1/mha/repl?conf=` | `masterha_check_repl` |
| PUT/POST | `/v1/mha/conf` | 写入 app.cnf（须在 conf-root 下，权限 0600） |
| POST | `/v1/mha/switch/planned` | `masterha_master_switch --master_state=alive` |
| POST | `/v1/mha/switch/dead` | `masterha_master_switch --master_state=dead` |
| GET | `/v1/mha/manager/status?conf=` | systemd is-active |
| POST | `/v1/mha/manager/start\|stop\|restart` | 写 unit 并 systemctl |
| POST | `/v1/mha/package/install` | 下载校验后 `rpm -Uvh` / `dpkg -i`（local 或 SSH） |
| GET | `/v1/mha/package/jobs/:id` | 装包任务进度 |

`conf` 必须落在 `-conf-root` 目录下。systemd unit 名默认 `mha-manager@<conf-stem>.service`。

## 平台登记

1. 高可用 → 登记 HA 控制器：引擎 MySQL、类型 `mha`、Base URL=`http://<manager>:8088`（或选用平台 Agent）
2. 「MHA 版本」登记 manager/node 分包并绑定 OSS → 在「MHA 配置」对集群执行安装
3. 登记集群：模式「复制 + MHA」，**1 主 ≥1 从**；生成/下发 app.cnf → 启停 manager
4. 发起计划切换或主宕机强制切换变更单并审批；平台编排 stop → switch → start 并回写角色
