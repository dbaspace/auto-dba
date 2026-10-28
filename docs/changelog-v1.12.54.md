# KeePlus / auto-dba v1.12.54

相对 [v1.12.53](changelog-v1.12.53.md)。

## 主机终端

- WebSocket 首次未能 OPEN 时提示「请重新从平台打开」，同一地址不再重连，避免失效会话刷屏。
- 访问日志脱敏 `token`；`/terminal/ws` 失败握手按客户端 IP 每分钟最多一条（含 suppressed 计数）。

## 已登记库

- 列表底部分页支持切换每页条数（10 / 20 / 50 / 100）。

## 打包与发版

```bash
STAMP=v1.12.54 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.54
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
