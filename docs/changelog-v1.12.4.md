# KeePlus / auto-dba v1.12.4

相对 [v1.12.3](changelog-v1.12.3.md)：一体包与后端包打入 SSH 下发用 Agent，避免控制面缺制品。

## 打包与部署

- `scripts/pack_release.sh` 的 **all** / **backend** 会交叉编译并打入：
  - `deploy/node-agent/bin/node-agent`（:8091）
  - `deploy/db-agent/bin/db-agent`（:8092）
  - `deploy/backup-agent/bin/platform-agent`（及兼容名 `backup-agent`）
- **frontend** 包仍仅静态资源，不含 Agent。
- Docker 控制面镜像同步 COPY 上述制品到 `/app/deploy/*/bin`；工作目录为 `/app` 时与路径解析一致。
- 解压后须以**包根**为工作目录启动，集群部署 SSH 下发无需再本机 `./deploy/*/build.sh`。

## 打包与发版

```bash
STAMP=v1.12.4 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.4
```

部署见 [deploy.md](deploy.md)。
