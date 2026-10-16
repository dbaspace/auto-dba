# KeePlus / auto-dba v1.12.6

相对 [v1.12.5](changelog-v1.12.5.md)。

## 飞书通知

- 接收人不再一律当作飞书 `user_id`：按格式识别邮箱、`open_id`（`ou_`）、`union_id`（`on_`）、群 `chat_id`（`oc_`）、手机号（通讯录换成 user_id）；也支持 `email:` / `mobile:` / `user_id:` / `open_id:` 前缀。
- 工单审核人与告警接收人按 **im_account → 邮箱 → 手机号** 依次尝试；飞书判定 ID 无效时换下一个候选。单人失败不影响其他人。
- 开启「工单创建通知审批人」后：所有工单提交 / 转移时通知审核人；**建库工单**执行成功并授权后，另向申请人私聊地址、库名、账号、密码（账号已存在为「沿用原密码」）、权限与说明。其他工单不向申请人发飞书结果。
- 建库账号已存在时，账号管理复用原加密密码，避免与实例上实际密码不一致。

应用需开通 `im:message`；按手机号发送另需 `contact:user.id:readonly`，且可用范围覆盖接收人。详见 [nacos-config.md](nacos-config.md)。

## 打包与发版

```bash
STAMP=v1.12.6 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.6
```

部署见 [deploy.md](deploy.md)。
