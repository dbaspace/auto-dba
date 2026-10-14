# KeePlus / auto-dba v2.3.5

相对 [v2.3.4](changelog-v2.3.4.md) 的增强：主机从监控同步、StarRocks 登记体验，以及 Agent 下发对已运行服务的安全护栏。

## 配置中心 · 主机

- 实例管理「主机」Tab 支持「从监控同步」：`POST /instances/sync-hosts-from-monitor`。
- 扫描「监控目标注册」全部 endpoints，按规范化 IP/主机名去重后登记为 `db_type=host`（有多少登记多少，已存在跳过）。
- SSH 默认 `sysadmin:22`、私有可见（当前用户）；名称/审批人等不足项事后编辑补充。

## StarRocks 登记与 Node Agent

- FE 密码优先从「FE 管理」按主机匹配（`POST /feadmin/resolve-credential`）；登记时 Node Agent 可暂空。
- 登记页可「探测 FE 角色」；工作台 Node Agent 支持从监控目标批量 SSH 下发（`POST /sr-cluster/agents/deploy-batch`）。

## Agent 下发安全（已有集群）

- SSH 下发 / 重新下发 **仅**安装或重启 Agent 自身，不启停 FE/BE/DB、不改业务 systemd/二进制/数据盘。
- 拒绝将 9030/8030/8040/3306 等业务端口配为 Agent 监听；`fuser -k` 仅在约定端口带 **8090–8199**。
- 覆盖 Node Agent、平台 Agent、DB Agent；文档与下发弹窗已明示。

## 说明

- 功能说明见 [features.md](features.md)、[sr-cluster.md](sr-cluster.md)、[db-cluster.md](db-cluster.md)、[deploy.md](deploy.md)。
