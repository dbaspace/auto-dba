# 接入层代理（HAProxy / Nginx / LVS）

路径：集群部署 Hub → **接入层代理** 卡片（`/ops/cluster-deploy/haproxy|nginx|lvs`）。

与数据库引擎工作台并列：支持**自建部署**与**登记已有**。平台不内嵌代理进程；装机与 conf 经**平台 Agent**（`:8090`）完成——与 DB 引擎的 OSS 制品 + DB Agent（`:8092`）路径不同，按代理软件「发行版包」惯例。

## 平台管什么

| 平台管 | 平台不管 |
|--------|----------|
| 自建装机：yum/dnf/apt 安装白名单包（含 keepalived） | 在控制面进程内嵌代理 / 选主 |
| 代理服务登记、配置草稿、模板、版本与恢复 | 编写业务路由策略本身（模板可改） |
| 校验下发、systemd start/stop/reload（代理 + keepalived） | 代改 Real Server 的 lo VIP / arp_*（LVS DR） |
| VIP 占用校验（平台登记 + Agent 本机/网络探测） | 跨机房 ARP/路由策略本身 |
| LVS：IPVS / VIP 只读诊断 | Keepalived VRRP 选主细节（由现场 conf 决定） |

## Keepalived 与 VIP（三种代理统一）

三种代理部署**均安装并启用 keepalived**，VIP **必填**，且**至少主+备 2 节点**（各绑一台平台 Agent `:8090`）：

| kind | 流量面 conf | Keepalived 职责 | HA 节点数 |
|------|-------------|-----------------|-----------|
| haproxy | `/etc/haproxy/haproxy.cfg` | 仅 VRRP 接管 VIP | **恰好 2**（主备） |
| nginx | `/etc/nginx/nginx.conf` | 仅 VRRP 接管 VIP | **2～8**（可追加 backup） |
| lvs | `/etc/keepalived/keepalived.conf`（主配置） | VRRP + `virtual_server`（IPVS） | **恰好 2** Director；RS 走 backends |

**拓扑说明**：

- 部署/登记须提交 `nodes[]`：恰好一个 `role=master`，其余 `backup`；Agent 不可重复。
- 平台为各节点自动生成 **不同 priority**（master 最高）与 `state MASTER|BACKUP`（backup 带 `nopreempt`），共享同一 VIP / `virtual_router_id` / `auth_pass`。
- **VIP 同时仅 MASTER 持有**——多节点主要提升故障切换冗余，而非 Nginx 主动吞吐横向扩容。若要对多台 Nginx 做主动负载，请在上游再挂 LVS（本 Hub 的 LVS 卡片）或使用 DNS/外部 LB。
- 下发时：各节点写相同的代理 conf（LVS 则写各自 Keepalived）+ 节点专属 Keepalived，并 reload 双方。

**VIP 校验（不可用已存在地址）**：

1. **平台侧**：与已登记代理服务 VIP、HA 集群 VIP 冲突则拒绝。
2. **Agent 侧**（登记绑 Agent / 自建部署 / 变更 VIP）：本机网卡已绑定、或 arping/ping 探测网段已占用则拒绝。

## 自建部署 vs 登记已有

| 入口 | 行为 |
|------|------|
| **自建部署** | 各 HA 节点 Agent 装包（见下表）→ VIP 探测 → 生成 conf + 按节点 Keepalived → 可选下发并 `systemctl enable --now` → 登记 |
| **登记已有** | 现场已装好；登记时仍须填 VIP 与至少主+备 `nodes` |

| kind | 安装包 | systemd unit | 说明 |
|------|--------|--------------|------|
| haproxy | `haproxy` + `keepalived` | haproxy、keepalived | TCP/HTTP + VIP |
| nginx | `nginx` + `keepalived` | nginx、keepalived | stream/http + VIP |
| lvs | `keepalived` + `ipvsadm` | keepalived | 顺带 `modprobe ip_vs*`（尽力） |

## 操作流程

1. 配置中心登记**平台 Agent**（装在代理机；需 root/sudo 装包权限）。
2. Hub 进入工作台 → **自建部署** 或 **登记已有**（填写未占用的 VIP 与 VRRP 参数）。
3. 编辑配置 → **保存草稿**（记版本）→ **校验并下发**（可选 reload；同步 Keepalived）。
4. LVS：可用「查看 IPVS」核对表项与 VIP。

## Agent API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/proxy/deploy` | 异步装包/启用（`kind` / `vip` 必填 / `enable_start` / `skip_install`） |
| GET | `/v1/proxy/deploy/jobs/:id` | 部署任务状态 |
| POST | `/v1/proxy/vip/check` | VIP 本机/网络占用探测 |
| PUT | `/v1/proxy/conf` | 写 conf（可 `validate`） |
| POST | `/v1/proxy/validate` | 仅校验 |
| GET/POST | `/v1/proxy/service/...` | status / start / stop / reload / restart |
| GET | `/v1/proxy/lvs/ipvs` | 只读 IPVS |
| GET | `/v1/proxy/lvs/status` | Keepalived + VIP |

允许 conf 路径：`/etc/haproxy`、`/etc/nginx`、`/etc/keepalived`、`/etc/auto-dba/proxy` 及 Agent workdir。

## 平台 API

前缀：`/api/v1/proxy`

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/services/deploy` | 自建部署（VIP 校验 + 触发 Agent + 登记） |
| GET | `/services/:id/deploy/jobs/:jobId` | 查询部署任务 |
| GET/POST | `/services` | 列表 / 登记已有（VIP 必填） |
| … | 其余同前（preview / push / versions / restore / lvs） | |

## 配置版本与恢复

变更（create/save/push/restore）写入版本表（最多 50 条）。工作台「版本历史」可查看与恢复；恢复可勾选同时下发 reload（含 Keepalived）。恢复会按当时 VIP/后端重建 Keepalived 副配置。

## LVS 说明（对齐 Keepalived/IPVS 官方模型）

kind=`lvs` 下发 **Keepalived** 配置：`global_defs` + `vrrp_instance` + `virtual_server`（`lb_algo` / `lb_kind=DR|NAT|TUN`）。

| lb_kind | 说明 | 平台边界 |
|---------|------|----------|
| **DR**（默认） | Director 改写 MAC | RS 需 lo VIP + arp_*；模板注释有参考，**不代改 RS** |
| **NAT** | SNAT/DNAT | 需转发与防火墙；RS 网关指向 Director |
| **TUN** | IPIP | RS 需隧道接口 |

参考：[Keepalived configuration synopsis](https://www.keepalived.org/documentation/user-guide/configuration-synopsis/)
