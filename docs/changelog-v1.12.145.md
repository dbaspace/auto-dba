# KeePlus / auto-dba v1.12.145

相对 [v1.12.144](changelog-v1.12.144.md)。归档在已绑定执行器的环境里改由 SQL 执行器跑。查询页签按账号留在本机。登记数据库账号必须填写现有密码。

## 归档

- 环境已绑定 SQL 执行器时，归档任务提交到执行器的 `/v1/archive/jobs`，平台轮询进度。未绑定执行器时不从平台本机直连。
- 须用本版包重下发 SQL 执行器。旧执行器没有归档接口，任务会失败。node-agent、db-agent、platform-agent 协议未改。
- 去掉「归档任务」申请页和侧栏入口。新建仍从归档控制台或任务管理进入。重启平台后会删掉已有库里的该菜单。
- 任务管理里，查看只读，编辑可改条件和调度；已停止的任务可以启用。控制台和执行记录展示失败原因。
- 归档工单审批通过后触发执行。定时任务显示等待定时执行，其余显示审批通过后立即执行。

## 查询与工单

- 通用查询的页签、SQL 和布局按用户和租户存在本机 localStorage。刷新或重新登录仍在。退出登录不再清掉这份内容，只清 sessionStorage 里的旧页签。
- 数据导出的 DBA、安全审批人：选了实例后用库或实例所属租户的活跃成员，不用登录租户。该租户没有人时回退建库审批人池。未选实例时仍用当前登录租户。

## 账号登记

- 「新建账号」改为「登记账号」。只把已有账号记到平台，不在数据库上建用户，也不自动生成密码。新建时必须填写该账号现有密码。编辑时留空仍表示不改密码。

## 打包与发版

```bash
STAMP=v1.12.145 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.145 -m "v1.12.145"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。**须用本版包重下发 SQL 执行器**，否则归档不会在执行器上跑。其它 Agent 协议未改；若它们仍是 v1.12.123 及更早，须用 v1.12.124 或本版包重下发。归档、审批人和账号登记要使用本版平台进程，请重启平台。

元库在重启时补两列。跳过全量迁移的已有库可先执行：

```sql
ALTER TABLE tbl_archive_runs ADD COLUMN IF NOT EXISTS executor_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tbl_archive_runs ADD COLUMN IF NOT EXISTS executor_job_id TEXT NOT NULL DEFAULT '';
```
