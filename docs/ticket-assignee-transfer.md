# 工单审核人转移与超级审核白名单

## 审核人转移

保持**单审核人**模型。待审批（`status=pending`）且审核人尚未操作时，**仅提交者**可将审核人换成另一人。

| 项 | 说明 |
|----|------|
| 入口 | 工单详情 → 待审批 →「更换审核人」 |
| API | `PUT /api/v1/tickets/:id/assignee`，body `{ "assigned_to": "<username>" }` |
| 候选人 | 与建单同源：`GET /tickets/target-approvers` 或 `GET /create-db/approvers`；前端排除当前审核人与提交人本人 |
| 迁移工单 | `target-approvers?issue_type=migration` 在 `migration_approver_user_ids` 已配置时与之取交集；转移时同时校验白名单与源实例/库审批人 |
| 审计 | `assignee_transfer_history` 追加 `旧->新 by=提交者 at=RFC3339` |
| 通知 | 仅通知新人（邮件，依赖通知配置中的工单创建开关） |

常见错误：非提交者 403；非 pending / 审核人已批 400；新审核人不在名单 / 与当前相同 / 为提交人本人 400。

弹窗区分三种状态：加载中；加载失败（黄色提示，可重试）；无其他候选人（蓝色提示，确认按钮禁用）。

## 全局超级审核白名单

| 项 | 值 |
|----|-----|
| 全局参数 key | `issue_global_approver_whitelist` |
| value | 登录名，逗号或换行分隔，如 `alice,bob` |
| 生效 | 审批/待办现读，改完无需重启 |
| 范围 | 全部 pending 工单 + 迁移任务审批 |

名单用户：

- 可在待办看到全部 pending 工单（不依赖 `assigned_to`）
- 可通过/驳回任意 pending 工单与迁移审批
- **不能**代提交者做「更换审核人」
- 审批时不改写 `assigned_to`；history 可记 `approved_by_whitelist=...`

配置入口：配置中心 → 全局参数。

## 迁移任务改派

`PUT /api/v1/migration/tasks/:id`，body 可用 `assigned_approver` 或 `assigned_approver_login`。

| 审批子状态 | 谁可改 |
|------------|--------|
| draft / 未提交 | 可改配（原规则） |
| pending | **仅创建人**；不可同人、不可空、不可为创建人本人；须在 `migration_approver_user_ids`（若已配置）且在源实例/库审批人中（若已配置） |
| 其它 | 不可改审核人 |

关联 pending issue 时会同步 `assigned_to`。
