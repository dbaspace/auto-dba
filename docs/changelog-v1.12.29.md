# KeePlus / auto-dba v1.12.29

相对 [v1.12.28](changelog-v1.12.28.md)。无表结构变更。工单申请环境档来自 Nacos/本地 YAML 的 `ticket.env_groups`，热更后下一请求生效。

## 工单申请环境档

- DDL / DML / 建库 / 权限申请 / DQL 导出 / Redis / Kafka 申请页把环境收成「生产 / 测试」，并按所选档过滤数据源。未写入任何一组的编码归「其他」。
- 省略 `ticket.env_groups` 时：编码含 `test` / `dev` 或等于 `uat` 归测试，其余归生产。
- 不改实例、环境管理、域名上的具体 `env_code`。SQL 检测和审核人仍按选中的数据源，不按整组执行。
- 接口：`GET /api/v1/tickets/env-groups`。配置示例见 [nacos-config.md](nacos-config.md)。

## 打包与发版

```bash
STAMP=v1.12.29 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.29
```

部署见 [deploy.md](deploy.md)。
