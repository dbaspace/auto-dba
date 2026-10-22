# KeePlus / auto-dba v1.12.28

相对 [v1.12.27](changelog-v1.12.27.md)。启动时幂等补 DNS 菜单与全局参数（`dns_approvers` 等）。DNS 记录存在独立解析库，连接见配置 `dns.dsn`。方案见 [dns-plan.md](dns-plan.md)。

## DNS 管理

- 侧栏 **DNS管理**：解析记录、域与范围、节点、变更记录、解析测试、解析体检。
- 保存后写入 `hdns` 表并递增 `serials`，节点约数秒内自行重载。集群部署可下发裁剪编译的 CoreDNS。
- 需审批的变更走工单类型 `dns_change`，审批通过后自动执行；详情展示变更对比，支持按批次回滚。

## 其它

- **实例总览**补充云监控存储使用率。
- 审核规则值支持 JSON 编辑，以及大小写（不限 / 小写 / 大写）。
- 修改密码后下线该账号已有会话（Redis 会话直接踢掉；未启用 Redis 时旧 JWT 在鉴权处拒绝）。
- 通用查询库表树做本地缓存，减少重复展开请求。

## 打包与发版

```bash
STAMP=v1.12.28 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.28
```

部署见 [deploy.md](deploy.md)。
