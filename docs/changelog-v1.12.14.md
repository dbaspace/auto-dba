# KeePlus / auto-dba v1.12.14

相对 [v1.12.13](changelog-v1.12.13.md)。

## 实例监控

- 平台 admin，以及 dba 租户的有效成员，实例/集群下拉不受当前租户限制，可查看其他租户的监控实例。
- 实例标签在没有 `db=` 时回退到 alias/label，再回退到实例名，避免北京等环境标签下拉不全。
- 图例 max/current 与数值列对齐；卡片变窄时图例改到图下方，避免 current 被裁切。

## 租户与资源配置

- 编码为 `dba` 的租户与默认租户一样视为系统租户，不可删除。
- 租户管理进入与刷新只拉租户列表；用户与角色改到打开「成员」时再加载。
- 资源配置实例列表增加环境筛选，并紧挨关键词。

## 打包与发版

```bash
STAMP=v1.12.14 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.14
```

部署见 [deploy.md](deploy.md)。
