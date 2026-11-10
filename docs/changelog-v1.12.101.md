# KeePlus / auto-dba v1.12.101

相对 [v1.12.100](changelog-v1.12.100.md)。数据库查询窗口按光标位置提示关键字、表名和字段。同步槽重启能读回采集时记下的连接器。云账号列表里过长的地域、环境和 SecretId 可以展开。无 Agent 协议变更，无表结构变更。

## 数据库查询

- 打开某个库的查询窗口后，`FROM`、`JOIN` 和逗号后面按已加载的表名过滤。`SELECT`、`WHERE`、`AND`、`OR`、`ON`、`USING`、`ORDER BY`、`GROUP BY` 后面提示本句写到的表的字段，表名写在光标后面也算。`表名.` 或 `别名.` 只提示这一张表。子查询不用外层的表。
- 关键字和常用函数按库类型在本地给出。MySQL、TiDB、StarRocks、OceanBase 用一套，PostgreSQL 用另一套。不访问目标库。
- 字段候选项带类型、是否可空和注释。重名列插入 `别名.字段`。两张表的同名列在 `ON` 后给一条等值条件，不查外键。关键字或含特殊字符的名字按该库的引号包起来。
- 输入一个字母或 `Ctrl / ⌘ + Space` 打开候选，最多 50 条。Tab 或点击填入。方向键移动后 Enter 填入，没移动时 Enter 仍是换行。Esc 关闭。`Ctrl / ⌘ + Enter` 仍是运行。
- 字段接口是 `GET /databases/:id/columns?table=`。先确认当前用户能看这个库，再连目标库。一次一张表、一条字典查询，5 秒超时，失败不重试，不进 PAT 白名单。打开窗口不预拉全库字段。当前语句里每张表在这次查询会话内最多查一次。MySQL 族和 PostgreSQL 可查字段，PostgreSQL 接受 `schema.table`；其它类型只提示表名。

## 同步槽重启

- 按槽名或记录读取同步槽时，带上采集补齐的 Connect 地址、connector、任务名和 topic。没有本地 CDC 任务的槽，重启可以继续用已记下的连接器。
- 槽上还没有任何 connector 时，重启跳过并提示先点刷新采集，让 Connect 补齐后再重启。

## 云账号

- 云账号列表里，云地域、平台区域、平台环境和 SecretId 过长时先收起，可展开查看。

## 打包与发版

```bash
STAMP=v1.12.101 ./scripts/pack_release.sh all
# 本地标签（本发版未 push / 未发 GitHub Release）：
# git tag -a v1.12.101 -m "v1.12.101"
```

部署见 [deploy.md](deploy.md)。一体包含本版构建的全部 Agent 二进制。本次未改 Agent 协议，已下发的 Agent 不必为这个版本重装。查询字段提示和同步槽重启要使用本版平台进程，请重启平台后再用。
