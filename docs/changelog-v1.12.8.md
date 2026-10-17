# KeePlus / auto-dba v1.12.8

相对 [v1.12.7](changelog-v1.12.7.md)。

## 退出登录

- 主动「退出登录」或闲置超时退出后直接回到登录页，不再弹出多条 `unauthorized`。
- 已在登录页时，残留请求返回的 401 一律静默。
- 使用中会话失效（如同一账号在别处登录被挤下线）只提示一次并跳转登录页，后续并发 401 不再叠加。

## 打包与发版

```bash
STAMP=v1.12.8 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.8
```

部署见 [deploy.md](deploy.md)。
