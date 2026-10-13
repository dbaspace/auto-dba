# 前端生产部署

> **完整打包与部署说明（一体 / 仅后端 / 仅前端 / Docker、端口对照）已统一到 [deploy.md](deploy.md)。** 本文保留最短路径，避免与开发态 `:5174` 混淆。

## 要点

- **生产不要开 5174**。5174 只给本机 `pnpm -C frontend dev`。
- 一体包或 Docker 开启 `http.serve_static: true` 后，页面与 API 都在 **`:8800`**。
- CentOS 7：**构建机** `./scripts/pack_release.sh all`，运行机只解压启动，不装 Node。

```bash
./scripts/pack_release.sh all
# → dist/auto-dba-release-linux-amd64-*.tgz
```

详见 [deploy.md](deploy.md)。
