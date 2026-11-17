# KeePlus / auto-dba v1.12.140

相对 [v1.12.139](changelog-v1.12.139.md)。Redis 工单和查询按域名管理取地址和审核人。管理员、审核白名单或 dba 租户代审时，审批流程记下实际操作人。首屏脚本拆开并 gzip。无 Agent 协议变更。

## Redis 地址和审核人

- Redis 工单和 Redis 查询只展示当前租户在域名管理里能看到的 Redis 域名。查询不再回退到实例上的域名或连接主机。
- 域名管理里只有 Redis 行可以填写审核人。一键同步不覆盖已填审核人，也不把实例审核人抄到每条域名上。
- Redis 工单的审核人是这条域名的审核人，再加上平台管理员、dba 租户和审核白名单。不再读取实例上的审核人。
- 提交后更换审核人使用同一名单。其它引擎的地址和审核人不变。

已有库在元数据库执行后再重启：

```sql
ALTER TABLE tbl_access_domains ADD COLUMN IF NOT EXISTS approvers TEXT NOT NULL DEFAULT '';
```

## 代审批

- 平台管理员、审核白名单或 dba 租户成员代替指定审批人通过或驳回时，记下操作人、所代的人和身份。
- 工单详情的审批卡片和底部工作流显示「由某人以某身份代审批」或「代驳回」。指定人自己审批不写这条。谁能审批没有变化。

## 首屏

- 页面脚本按路由拆开。react、antd、echarts 单独成块，只改业务时这三块的缓存可以继续用。
- 平台进程对 js、css、html、svg 按浏览器支持返回 gzip。区间请求仍返回未压缩内容。

## 打包与发版

```bash
STAMP=v1.12.140 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.140 -m "v1.12.140"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议。已经用 v1.12.124 及以后包下发过的 SQL 执行器不必为这个版本重装；若执行器仍是 v1.12.123 及更早，须用 v1.12.124 或本版包重下发。Redis 域名审核人、代审记录和静态 gzip 要使用本版平台进程，请重启平台。
