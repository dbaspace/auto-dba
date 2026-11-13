# KeePlus / auto-dba v1.12.129

相对 [v1.12.128](changelog-v1.12.128.md)。Redis 查询改为执行只读命令，危险命令仍然拒绝。登录页可按开关提交自助注册，管理员审批后才建用户。从数据库查询进入导出时，按已登记库的域名补选数据源。有表结构变更，无 Agent 协议变更。

## Redis 查询

- 查询页可执行命令目录中的只读命令，包括 `LLEN`、`PTTL`、`DBSIZE`、游标扫描 `HSCAN` / `SSCAN` / `ZSCAN`，以及 `OBJECT`、`MEMORY USAGE`、`SLOWLOG GET` 和 Stream 的 `XLEN`、`XRANGE`、`XREVRANGE`、`XINFO`。
- `GET`、`INFO`、`SCAN` 的结果格式不变。`HSCAN`、`SSCAN`、`ZSCAN` 的 `COUNT` 最大仍是 1000。
- `KEYS *`、`CONFIG`、`FLUSHALL` 等危险命令在连接 Redis 之前拒绝。写命令、`EVAL`、`SORT`、`XREAD` 不能在查询页执行。
- `OBJECT` 只接受 `ENCODING`、`IDLETIME`、`REFCOUNT`、`FREQ`。`MEMORY` 只接受 `USAGE`。`SLOWLOG` 只接受 `GET`。

## 自助注册

- 系统通知设置增加「开放注册」。关闭时登录页不能注册。
- 注册只提交申请，管理员在用户管理里通过或驳回后才创建账号。通过前不写入用户表。

## 数据导出

- 从数据库查询带入导出时，已登记库只有一条域名则自动选中；多条时留给导出页按当前环境再选。库没有绑定域名则不选实例主域名。

## 打包与发版

```bash
STAMP=v1.12.129 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.129 -m "v1.12.129"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议。已经用 v1.12.124 及以后包下发过的 SQL 执行器不必为这个版本重装；若执行器仍是 v1.12.123 及更早，须用 v1.12.124 或本版包重下发。Redis 查询和自助注册要使用本版平台进程与前端，请重启平台并刷新页面。已有元库需补齐下方表结构后再重启。

```sql
CREATE TABLE IF NOT EXISTS tbl_user_registrations (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL
);
ALTER TABLE tbl_system_notices ADD COLUMN IF NOT EXISTS user_register_enabled INTEGER NOT NULL DEFAULT 0;
```
