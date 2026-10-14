# KeePlus / auto-dba v1.1～v1.10 更新点汇总

本文汇总原 GitHub Release **v1.1.0～v1.10.0** 的要点（这些中间版本的 Release/标签已下线，能力并入后续正式包）。早期包见 [v1.0.0](https://github.com/dbaspace/auto-dba/releases/tag/v1.0.0)。当前推荐下载见 [deploy.md](deploy.md) 与最新 Release（如 [v2.3.5](changelog-v2.3.5.md)）。

## 打包与部署（原 v1.1.0）

- 一体包 / 仅后端 / 仅前端 / Docker 多种打包方式（`scripts/pack_release.sh`）
- SPA `serve_static` 白屏硬化（缺失资源 404、index 禁缓存等）
- 部署说明见 [deploy.md](deploy.md)、[faq-spa-white-screen.md](faq-spa-white-screen.md)

## 多租户与顶栏（原 v1.2.0～v1.3.0）

- 租户删除：无成员且无绑定资源时可删；权限 `um.action.tenants.delete`
- 租户管理列表一屏布局
- 平台管理员创建/切换业务租户时保留默认租户成员
- 顶栏稳定显示租户「名称」；删除当前会话租户后自动切回可用租户

## 品牌与集群划拨（原 v1.4.0）

- KeePlus `favicon.ico` / apple-touch 图标
- db-deploy 同名集群可划拨
- 删除集群工单预填审批人，审批结束后刷新列表

## 终端与 StarRocks（原 v1.5.0）

- 一体/后端包打入 `scripts/terminal` 与 `assets/terminal`
- StarRocks 启停失败不污染集群 `status`（`failed` 仅表示初始化/重建失败）

## 实例管理 UX（原 v1.6.0～v1.8.0、v1.10.0）

- 切换 PostgreSQL 等库类型不再回跳 MySQL（类型未就绪时保留 URL `db_type`）
- 按 `db_type` 多 Tab 并存
- 列表域名、标签折叠为前 2 个 +N Tooltip
- 域名支持多行粘贴拆条，按规范化键去重
- 列表支持列排序与底部分页（对齐已登记库交互）

## 工单数据源按域名（原 v1.9.0）

- DDL / DML / 建库 / 权限申请：数据源按实例 `domains[]` 展开，标签为域名（非 host:port）
- 选中域名后逻辑库按绑定域名过滤
- 提交写入工单 `domain`，执行连接 Host 使用所选域名
- `tbl_issues.domain` 列；migrate skip 路径幂等补齐

## 版本对照（历史）

| 原版本 | 要点（摘要） |
|--------|----------------|
| v1.1.0 | 一体/分服务打包；SPA 白屏硬化；部署文档 |
| v1.2.0 | 租户删除；租户列表布局；删除权限 |
| v1.3.0 | 切换租户保留默认成员；顶栏租户名；会话租户回退 |
| v1.4.0 | favicon；db-deploy 同名划拨；删集群工单审批人 |
| v1.5.0 | 终端脚本入包；SR 启停不污染集群 status |
| v1.6.0 | 实例管理切库类型不回跳 MySQL |
| v1.7.0 | 多 Tab；域名列折叠 |
| v1.8.0 | 标签列折叠；域名多行粘贴去重 |
| v1.9.0 | 工单数据源按实例域名选择 |
| v1.10.0 | 实例列表排序与分页 |
