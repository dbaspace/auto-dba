# KeePlus / auto-dba v1.12.83

相对 [v1.12.82](changelog-v1.12.82.md)。启动时自动给登录日志补「内网 IP」列，并把原先写在登录 IP 上的内网地址挪过去。无 Agent 协议变更。

## 登录日志

- 登录 IP 记公网地址，内网 IP 另记一列。登录页会先向公网回显地址查询当前出口（默认 `https://api.ipify.org?format=json`，可用 `http.public_ip_echo` 改），账号登录和 SSO 都会带上这个地址。
- 浏览器没查到公网地址时，仍从本次请求的代理链里取第一个公网地址；只有内网地址时，登录 IP 留空，地址写到内网 IP。上报的内网或回环地址不会当成公网。
- 登录日志页增加「内网 IP」列。历史记录里误写在登录 IP 上的内网地址会挪到内网 IP，登录 IP 清空；原先没有公网出口的记录不补写。

## 打包与发版

```bash
STAMP=v1.12.83 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.83 -m "v1.12.83"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 行为，已下发的 Agent 不必为这个版本重装。
