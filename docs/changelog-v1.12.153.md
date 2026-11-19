# KeePlus / auto-dba v1.12.153

相对 [v1.12.152](changelog-v1.12.152.md)。归档补齐直接删除、分片删除、Python/Go 脚本、服务端告警和同实例互斥。已登录页面会按新的闲置分钟数计时。

## 归档

- PostgreSQL 直接删除走 CTE，每批 10000 行，在事务里数完返回行再提交。某一批影响行数为 0 时不提前结束。没有主键时仍按 ctid 删除。MySQL 仍是 `DELETE ... LIMIT 10000`。
- 显式分片，或表有 `create_time` 且估计行数不少于 10 万时，按小时切分，导出主键后再每批 5000 行删除。没有主键或没有 `create_time` 时报错，不静默退回整表删除。
- 自定义脚本支持 Python 和 Go。提交前检查语法；Go 会在临时目录编译。可按主机、库、表和保留天数生成 Go 删除脚本。Shell 仍拒绝。SQL 脚本仍按语句执行。
- 告警在平台侧判断运行超时、当日失败次数、存储用量和断连。事件落库，可确认。同一指纹当天只通知一次。邮件和 Webhook 按归档设置真实发送，页面上的测试也会真的发出。
- 同一实例同一时间只允许一个归档在执行。锁写入元库，超过 2 分钟且任务已不在运行的旧锁会被清掉。
- CTE、分片和脚本在 SQL 执行器进程里跑。**须用本版包重下发 SQL 执行器**。告警和实例锁在平台侧，重启平台即可。node-agent、db-agent、platform-agent 协议未改。

## 登录

- 已打开的页面每 60 秒读取 `login_idle_timeout_min`。改短并且距离上次操作已超过新时限时马上退出；改长则仍从上次操作接着算；改为 0 则停止因闲置退出，之后再改成大于 0 时从当时重新计时。这次读取不刷新最后活动时间。绝对会话窗口仍默认 24 小时。

## 打包与发版

```bash
STAMP=v1.12.153 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.153 -m "v1.12.153"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。**须用本版包重下发 SQL 执行器**，否则 CTE 批次、分片删除和 Python/Go 脚本仍是旧执行器上的行为。其它 Agent 协议未改；若它们仍是 v1.12.123 及更早，须用 v1.12.124 或本版包重下发。归档告警、实例锁和闲置超时要使用本版平台进程，请重启平台。

元库在重启时补告警规则、告警事件和实例锁三张表。跳过全量迁移的已有库可先执行：

```sql
CREATE TABLE IF NOT EXISTS tbl_archive_alert_rules (
    id TEXT PRIMARY KEY,
    rule_type TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    threshold DOUBLE PRECISION NOT NULL DEFAULT 0,
    severity TEXT NOT NULL DEFAULT 'warning',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_alert_rule_type ON tbl_archive_alert_rules(rule_type);

CREATE TABLE IF NOT EXISTS tbl_archive_alert_events (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL DEFAULT '',
    rule_type TEXT NOT NULL DEFAULT '',
    fingerprint TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'warning',
    acked INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_archive_alert_fp ON tbl_archive_alert_events(fingerprint);

CREATE TABLE IF NOT EXISTS tbl_archive_instance_locks (
    instance_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    acquired_at TEXT NOT NULL
);
```
