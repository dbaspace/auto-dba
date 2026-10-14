# KeePlus / auto-dba v2.3.4

相对 [v2.3.3](changelog-v2.3.3.md) 的增强与体验优化。

## 域名管理与 Kafka 查询

- 域名管理支持按环境 Select 与域名/实例/环境关键字过滤；`GET /access-domains` 的 `q` 可匹配 `env_code`。
- Kafka 查询侧栏改走本租户可见域名（`db_type=kafka`），无域名时回退实例列表。

## 登录与 Tab

- 登录页按认证方式维护多账号历史（可下拉选择），可选「记住密码」。
- 登出不再清空 `auto-dba-app-tabs:{userId}`；再次登录 `hydrateTabs` 恢复上次打开的 Tab。

## 集群部署：Agent 自动绑定

- StarRocks / DbDeploy 登记或填 Host 后，自动匹配或登记 Node Agent（`:8091`）/ DB Agent（`:8092`）。
- Proxy（HAProxy/Nginx/LVS）填 Host 后自动匹配平台 Agent（`:8090`，不静默创建）。
- StarRocks 登记已有集群：可「探测 FE 角色」（`POST /sr-cluster/probe-fe-roles`），自动标 LEADER/FOLLOWER 并将 Leader 设为首 FE。

## 配置中心 · FE 管理

- 「从监控同步」按监控目标 `starrocks-fe` 写入任务/环境/域名/端口/**全部 FE 节点**；不覆盖已有账号密码。
- 表单与列表支持多 FE 展示；同步后可再编辑补充凭据。
- 允许无密码创建 FE 凭据（同步场景）。

## 说明

- 功能说明见 [features.md](features.md)、[sr-cluster.md](sr-cluster.md)、[db-cluster.md](db-cluster.md)、[proxy.md](proxy.md)、[tenancy.md](tenancy.md)、[deploy.md](deploy.md)。
