# KeePlus / auto-dba v1.12.66

相对 [v1.12.65](changelog-v1.12.65.md)。

## 迁租后会话自愈

- **`GET /auth/me`**：JWT `tid` 已非有效成员时（常见于普通用户加入业务租户后自动离开 default），自动换票到剩余唯一租户（`token` + `tenant_session_rebound`）；多租户返回 `need_tenant_select`。
- **前端**：`AppLayout` 落盘新 token 并重置 Tab；需选租户时跳转选租户页。
- **管理端**：保存成员后提示对方刷新页面。
- 避免迁租后仍停在 default 导致空菜单 / 业务 API 403。

## 打包与发版

```bash
STAMP=v1.12.66 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.66 -m "v1.12.66"
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
