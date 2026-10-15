# KeePlus / auto-dba v1.11.0

版本线自本包起重新以 **v1** 为主推 Release（原 GitHub 上的 v2.x Release/标签已下线；历史说明仍保留在仓库内 [changelog-v2.*.md](changelog-v2.3.7.md)）。相对此前 v2.3.7 能力基线，本包主要增量如下。

## 平台资源申请

- 新工单类型 `resource_apply`（侧栏 `/ops/resource-apply`）：「资源可见」与「库审核人」两套意图。
- **资源可见**按归属自动分支（服务端强制重算）：
  - 归属为**默认租户** → **划拨**到申请人当前租户；
  - 归属为**其它租户** → **跨租只读授权（grant）**；
  - 与申请人**同租** → 私有资源写 ACL。
- **库审核人**：从本租成员多选目标用户，审批通过后写入库 `approvers`（私有库同步开通可见）；工单审批人为平台管理员 ∪ `issue_global_approver_whitelist`。
- 含默认租户划拨项时，工单审批人须为平台管理员。
- 详见 [ops-manual.md](ops-manual.md)、[tenancy.md](tenancy.md)、[features.md](features.md)。

## 平台资源申请 · 跨租审核

- 平台管理员与超级审核白名单可在任意当前租户会话下，查看并审批**全平台** `resource_apply` 工单（列表 / 待办 / 详情），无需加入业务租户或 impersonate。
- **不**扩大到 DDL/DML 等其它工单类型的跨租可见性。

## 登录锁定与解锁

- LDAP/SSO 失败冷却（10 分钟）按用户名隔离；登录页冷却中可更换其它账号。
- 切换用户名或认证方式时清除本地验证码展示（平台账号登录不因上一 LDAP 账号失败而残留验证码）。
- 用户管理：外部冷却中的用户显示「已锁定」；「解锁」同时清除平台锁定与 `tbl_login_attempt` 冷却，立即恢复登录。

## 其它

- 全局参数长值截断与「眼睛」查看；库审核人相关详情提示等体验优化。

## 打包与发版

```bash
STAMP=v1.11.0 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.11.0
```

以后发版统一用 [`scripts/publish_github_release.sh`](../scripts/publish_github_release.sh)（git credential → `GH_TOKEN` → `gh release create`）。部署见 [deploy.md](deploy.md)。
