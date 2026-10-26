# KeePlus / auto-dba v1.12.42

相对 [v1.12.41](changelog-v1.12.41.md)。启动时创建 `tbl_sql_executors`（已存在则跳过），并给下发表补 `kind`、`payload`。配置中心增加菜单「执行器」。

## SQL 执行器

- 配置中心「执行器」登记执行器并绑定环境 code。全局参数 `ticket_local_idc` 标出本服务所在环境，这些环境的 DDL/DML 仍在本进程直连。已绑定执行器的环境把 SQL 下发到对应执行器。本服务环境已配置、但该环境没有执行器时，不从控制面直连。
- 执行器进程是 `deploy/sql-agent`，默认监听 `:8093`。一体包、后端包和 Docker 镜像都会编译并带上 `deploy/sql-agent/bin/sql-agent`。页面 SSH 下发从包根读取这个文件，不必在目标机上再编译。
- 终止工单时，若语句跑在执行器上，终止请求发到该执行器。

## 打包与发版

```bash
STAMP=v1.12.42 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.42
```

部署见 [deploy.md](deploy.md)。
