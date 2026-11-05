# KeePlus / auto-dba v1.12.84

相对 [v1.12.83](changelog-v1.12.83.md)。登录日志的内网地址优先用浏览器读到的本机地址；读不到时仍记请求里的内网地址。无 Agent 协议变更。

## 登录日志

- 登录页用 WebRTC 收集本机 host 地址（不访问 STUN，最多等 2 秒），只提交第一个私网 IPv4。账号登录和 SSO 都会带上 `lan_ip`。
- 服务端只接受私网地址。公网、回环、链路本地上报会被丢掉。
- 浏览器没有上报时，内网 IP 回退为本次请求里的内网地址。公网地址仍优先用浏览器上报，否则用请求里的公网地址。

Chrome 默认会把本机地址藏成 `.local`。需要企业策略 `WebRtcLocalIpsAllowedUrls` 允许本站，页面才能读到真实内网地址。

## 打包与发版

```bash
STAMP=v1.12.84 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.84 -m "v1.12.84"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
