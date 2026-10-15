# KeePlus / auto-dba v1.12.0

相对 [v1.11.0](changelog-v1.11.0.md) 的通知渠道、告警频控与查询/工单体验增强。

## 通知渠道（邮件仍走 SMTP）

- Nacos `notify` 段配置飞书私聊 / 飞书告警应用 / 企微应用 / 短信 / 电话 / 群机器人 Webhook；热更重建 Provider。
- 认证配置 → 通知：按渠道分块开关事件与发送测试；密钥不进页面。
- 企微告警支持 `party_ids`（部门）；工单按审批人 `im_account`。
- 群机器人 URL 在 Nacos；**告警 / 工单方向在页面**按条配置（`robot_routes`）+「工单待审打群」。
- 详见 [nacos-config.md](nacos-config.md)、[features.md](features.md)。

## 告警外发频率

- 全局默认（通知页「告警发送频率」）：窗口内最多 N 次 trigger/续报（默认 **15m / 3 次 / 续报 5m**）；规则可继承全局或单独覆盖。
- 指纹级预算（`tbl_alert_notify_budget`）抑制抖动反复 trigger 刷屏。
- **告警最终恢复必发一条**「告警已恢复」，不占用频控额度；同生命周期至多一条。
- 详见 [alerts.md](alerts.md)。

### 升级 SQL（若启动未自动加列）

```sql
ALTER TABLE tbl_alerts
  ADD COLUMN IF NOT EXISTS resolve_notified INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS tbl_alert_notify_budget (
  fingerprint TEXT PRIMARY KEY,
  notify_count INTEGER NOT NULL DEFAULT 0,
  last_notified_at TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT ''
);
```

## 查询与工单

- 通用查询：结果行数限制、敏感列脱敏、收藏 SQL 等（见功能页与接口）。
- DQL 等多级审批相关加固与体验修复。

## 打包与发版

```bash
STAMP=v1.12.0 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.0
```

部署见 [deploy.md](deploy.md)。
