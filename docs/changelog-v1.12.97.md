# KeePlus / auto-dba v1.12.97

相对 [v1.12.96](changelog-v1.12.96.md)。配置中心把平台 Agent、SQL 执行器、Node Agent、DB Agent 收到同一页。PAT 查询写入查询审计，并限制为只读且 SELECT 必须自带 LIMIT。PostgreSQL DDL 执行时，后定义的函数会排到引用它的触发器之前。无 Agent 协议变更，无表结构变更。

## Agent 管理

- 配置中心菜单「平台 Agent」改为「Agent管理」（`/config/agents`）。页签为平台 Agent、SQL 执行器、Node Agent、DB Agent。旧地址 `/config/sql-executors` 打开后进入 SQL 执行器页签。独立的执行器菜单在启动时隐藏。
- StarRocks 与引擎工作台里原来的 Node Agent、DB Agent 页改为入口，登记和下发改到 Agent管理。集群仍按主机绑定对应 Agent。
- Node Agent 列表可按主机环境和地址筛选。地址匹配 URL、连接主机和域名。
- 平台 Agent、SQL 执行器、Node Agent 的批量探活整批最多 8 秒。单台超时记为未知，页面不再一直等全部拨号结束。

## PAT 查询

- `GET /api/v1/query/catalog` 进入机器通道白名单。授权和 PAT 都通过后，可以拉取当前用户可见的库目录。
- PAT 提交的 SQL 写入查询审计，包含执行前就被拒绝的语句。查询审计列表增加「令牌」列。目录接口本身没有 SQL，不记查询审计。
- PAT 不返回平台登记的实例密码和账号明文。
- PAT 的 `SELECT` / `WITH` 必须在语句末尾自带正数 `LIMIT`（含 `LIMIT n OFFSET m` 和 `LIMIT offset, count`）。子查询里的 LIMIT 不算。缺少时返回 400，并写入查询审计。浏览器查询页仍自动补默认 LIMIT。
- `SELECT` / `WITH` / `EXPLAIN` 正文里的 `INSERT`、`UPDATE`、`DELETE`、DDL 等写操作会被拒绝。`SHOW` 不按这条扫描。
- PostgreSQL、MySQL、TiDB 的 PAT 查询强制会话只读。StarRocks 不设置该会话变量。浏览器是否只读仍看全局参数 `client_access_harden_readonly_session`。
- `POST /api/v1/tickets/:id/execute` 仍不在白名单。PAT 不能执行工单。

## 工单

- PostgreSQL DDL 执行前，若 `CREATE FUNCTION` 写在引用它的 `CREATE TRIGGER` 之后，执行顺序把函数挪到该触发器之前。工单里保存的 SQL 不变。MySQL 和 DML 不调整。已经按语句提交过的工单，重跑时前面已成功的语句仍可能报已存在。

## 打包与发版

```bash
STAMP=v1.12.97 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.97 -m "v1.12.97"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。平台进程需重启后，PAT 限制、DDL 执行顺序、探活超时和菜单名称才生效。Agent管理页面与查询审计的令牌列刷新即可。
