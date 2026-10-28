# KeePlus / auto-dba v1.12.49

相对 [v1.12.48](changelog-v1.12.48.md)。

## Nacos 管理

- 控制台 JS/CSS/图片优先从包内 `assets/nacos-console` 提供，HTML/API 仍代理远程；可用 `scripts/sync-nacos-console-assets.sh` 同步。一体包 / 后端包 / Docker 会打入该目录（若存在）。
- 说明见 [nacos-config.md](nacos-config.md)。

## 工单与查询

- 多实例 DDL 审核人改为各数据源名单**并集**（后端与申请页一致）。
- DQL 导出 limit 硬顶提高到 100 万；导出须带有效 LIMIT。

## 实例总览与其它

- Redis CPU：近 5 分钟 Prometheus 迷你时序（`redis_cpu`）。
- 库划拨 / StarRocks 布局探测 / 认证凭据校验等小幅修复与测试。

## 打包与发版

```bash
STAMP=v1.12.49 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.49
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
