# KeePlus / auto-dba v1.12.43

相对 [v1.12.42](changelog-v1.12.42.md)。无新表。下发记录的 `kind` 会在平台 Agent 列表里显示。

## SQL 执行器

- 一体包、后端包和 Docker 镜像仍编译并带上 `deploy/sql-agent/bin/sql-agent`。下发时优先用包里的这个文件；包里没有时才在本机编译。
- 平台 Agent 的下发记录按类型区分「平台 Agent」和「SQL 执行器」。远程目录留空时用 `/home/<SSH 用户>/auto-dba/sql-agent`。不提权时目录必须是该用户可写的。
- 「执行器」页从下发成功记录里选一台登记，地址自动带出。本页不再填写账号和密码。探活仍检查地址是否可达，以及环境有没有绑到另一台执行器。

## 打包与发版

```bash
STAMP=v1.12.43 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.43
```

部署见 [deploy.md](deploy.md)。
