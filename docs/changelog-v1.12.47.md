# KeePlus / auto-dba v1.12.47

相对 [v1.12.46](changelog-v1.12.46.md)。

## Nacos 控制台

- 配置中心可按环境登记 Nacos 控制台地址与账号；运维页「Nacos 管理」同源 iframe 代登嵌入，默认打开配置管理。
- 说明见 [nacos-config.md](nacos-config.md)、[features.md](features.md)。

## SSH 与 Agent 下发

- 支持按环境绑定多套 SSH 私钥档案（`ssh_key_profiles`），终端 / 备份 / 集群 / 执行器下发按环境选用对应密钥。
- 执行器停远端、重下发与相关 Agent 部署路径沿用档案密钥。

## 云资源与其它

- 云 PostgreSQL / 云账号等页面与执行链路小幅增强；海豚管理改为按环境代登 iframe。
- 一体包仍含 `deploy/sql-agent/bin/sql-agent`。

## 打包与发版

```bash
STAMP=v1.12.47 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.47
```

部署见 [deploy.md](deploy.md)。
