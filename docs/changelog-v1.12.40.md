# KeePlus / auto-dba v1.12.40

相对 [v1.12.39](changelog-v1.12.39.md)。无表结构变更。`redis.sliding_expire` 仍可写在配置里，运行时已忽略。

## 登录会话

- 会话仍在有效期内时，鉴权会把 Redis 的过期时间续到当前时间 + 会话 TTL（默认 1 天，同一会话 1 分钟内不重复写）。到期后返回 `session_expired`，不再续期。
- JWT 剩余不足 12 小时时，用原来的 `jti` 换发，响应头 `X-Access-Token`，前端写回本地 token。
- 登录会话页说明与上述续期一致。闲置退出仍是另一套规则，先到先失效。

## DDL 工单

- 分库或多元数据源执行时，某个库或实例失败后继续其余目标，不再停在第一处失败。
- 既有成功又有失败时，汇总为「部分分库执行失败，成功 N 个库，失败 N 个库」。

## 打包与发版

```bash
STAMP=v1.12.40 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.40
```

部署见 [deploy.md](deploy.md)。
