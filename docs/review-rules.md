# 审核规则说明

配置入口：[配置中心 → 审核规则](/config/review-rules)（`/config/review-rules`）。

按引擎维护 DDL / DML（及 Redis 命令）规则；工单提交前由控制面按目标库类型套用。

## PostgreSQL：`DDLRequireUpdateTime`

建表必须满足「时间字段」规范（仅 PostgreSQL DDL；CTAS 不检查）。

| 配置项 | 说明 |
|--------|------|
| `enabled` | 是否启用 |
| `column` | 候选时间列名。**多个用逗号（或中文逗号）分隔，满足任一即可**；也支持 JSON 数组，如 `["modify_time","update_time"]`。默认 `update_time` |
| `allowed_types` | 允许的时间类型列表（如 `timestamp` / `timestamptz` 等） |

通过条件（同一工单内）：

1. `CREATE TABLE` 中至少有一个候选列，且类型在 `allowed_types` 内；
2. 存在对该表的 `BEFORE UPDATE ... FOR EACH ROW` 触发器；
3. **命中的那一列**有索引（`CREATE INDEX` / 列 `UNIQUE` / 表级 `UNIQUE`）。

示例：配置 `modify_time,update_time` 时，表只含 `modify_time`（合法类型 + 触发器 + 该列索引）即可通过；两列都没有则报错，文案会列出候选列名。

## 相关页面

更多能力总览见 [features.md](features.md)。
