# KeePlus / auto-dba v1.12.32

相对 [v1.12.31](changelog-v1.12.31.md)。无表结构变更，无新配置项。

## CDC

- 加表任务的 Routine Load 名为 `rl_` 加目标表名。链路状态、重新初始化和 SQL 预览都按任务类型取这个名字。
- 加表的 Debezium 预览在父任务已有 Connector 上追加 `table.include.list`，保留原来的 slot 和 publication。
- 初始化预检不再因为缺少 `debezium_signal` 失败；提交后自动创建并加入 Publication。业务表缺失仍然拦截。
- StarRocks 建表按 PostgreSQL 列类型映射（含长度、精度、JSON、日期时间），主键和不可空列写成 `NOT NULL`，并带上列注释。日期、无时区时间戳按 Debezium 的天数或毫秒写入 Routine Load。
- 信号表不进入业务建表和 Routine Load。

## 数据迁移

- PostgreSQL 迁移补齐扩展、序列（serial / identity / 外部 `nextval`）、索引。外键和触发器等全部表与数据完成后再建。引用了本次未迁移表的触发器会跳过。
- `GENERATED ALWAYS AS IDENTITY` 不走 COPY，插入时带 `OVERRIDING SYSTEM VALUE`。数据写完后把序列拨到目标表当前最大值。MySQL 同步 `AUTO_INCREMENT`。
- 生成列不参与拷贝。迁移详情页签显示任务名，返回时回到迁移列表。

## AI 助手

- 对上平台实例后，可以写一条只读 SQL（`SELECT` / `WITH … SELECT` / `SHOW` / `EXPLAIN`），并指定库名执行，不再退回维护库。
- 登记域名和连接主机指向同一台实例时按该实例继续查。脚本、`python` / `exec` 一类动作会被拒绝。

## 实例总览

- 刷新时显示采样进度；本轮结束后显示完成时间。采样过程中汇总数字随结果更新。

## 打包与发版

```bash
STAMP=v1.12.32 ./scripts/pack_release.sh all
# 或：PACK=1 ./scripts/publish_github_release.sh v1.12.32
```

部署见 [deploy.md](deploy.md)。
