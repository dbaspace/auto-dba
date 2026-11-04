# KeePlus / auto-dba v1.12.76

相对 [v1.12.75](changelog-v1.12.75.md)。无表结构变更。

## CDC 加表：审计会话父任务误报非 init

- **prepareCDCCreate** 按列表同一范围（`effectiveListTenantID`）解析父任务。平台管理员租户审计下可选到其它租户的 init 任务，提交不再因 JWT 租户查不到而报「要求 parent 为初始化任务」。
- 父任务不存在与不是 init 分开提示。
- 加表/加列等子任务与共享工单继承父任务 `tenant_id`；已带租户时 persist 不再用 JWT 覆盖。审批人按父任务租户解析。
- 非审计用户仍不能为其它租户的父任务创建加表任务。

## 打包与发版

```bash
STAMP=v1.12.76 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.76 -m "v1.12.76"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。须用本版包更新后端后，「新增同步表」步骤 4 才能在审计会话下提交成功。
