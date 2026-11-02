# KeePlus / auto-dba v1.12.62

相对 [v1.12.61](changelog-v1.12.61.md)。

## 客户端访问授权

- 开通授权改为从平台用户列表选择登录名（展示显示名），不再手工填写；成功后提示对方在「我的访问令牌」自行签发。

## 租户跨租授权

- 资源多选下拉支持关键字过滤与勾选态展示（交互对齐 DDL 数据源选择）。

## CDC 创建

- 加表/改结构等选父任务时支持按任务名、源库关键字过滤；父任务选择与表单同步逻辑整理，减少串态。

## DQL 下载

- 「下载文件」改为浏览器原生导航下载（带 `access_token` / cookie），大文件下载过程中刷新页面不中断 axios 请求。

## 打包与发版

```bash
STAMP=v1.12.62 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.62
```

部署见 [deploy.md](deploy.md)。一体包仍含 `deploy/sql-agent/bin/sql-agent`。
