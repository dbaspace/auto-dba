# Nacos Console 本地静态资源

由 `scripts/sync-nacos-console-assets.sh` 从远程控制台同步。布局与 URL 路径一致，例如：

- `next/js/*.js`
- `next/css/*.css`
- `next/img/*`

`nacos-proxy` 对上述静态 GET **本地优先**；HTML 入口与 API 仍走远程。重新同步：

```bash
CONSOLE_URL=http://<host>:8080/next ./scripts/sync-nacos-console-assets.sh
```

见 `docs/nacos-config.md`「运维嵌入：本地控制台静态资源」。
