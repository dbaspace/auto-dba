# KeePlus / auto-dba v1.12.7

相对 [v1.12.6](changelog-v1.12.6.md)。

## 登录会话

- 同一账号同时只保留**最新一次**登录：新登录会清空该用户 Redis 中旧会话，元库旧 online 行标为 `replaced_by_login`。
- 旧终端持有的 token 下次请求返回会话失效，需重新登录。
- 切换租户 / 审计进入签发新会话时同样只保留最新一条。

配置说明见 [nacos-config.md](nacos-config.md)（`redis.max_sessions_per_user` 保留兼容，运行时始终踢旧）。

## 打包与发版

```bash
STAMP=v1.12.7 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.7
```

部署见 [deploy.md](deploy.md)。
