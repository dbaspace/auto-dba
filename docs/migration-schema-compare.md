# 表结构对比

入口：**同步与迁移 → 表结构对比**（路径 `/ops/migration/compare`）。

对两侧已登记实例中的库表做**异步结构对比**，并按「源端 → 目标端」生成**只增不删**的修复 SQL（需人工审核后执行）。

## 适用引擎

| 族 | 类型 | 互比 |
|----|------|------|
| PostgreSQL | `postgres` / `postgresql` / `pg` / `pgsql` | 仅同族 |
| MySQL 族 | `mysql` / `tidb` / `oceanbase` | 仅同族 |

**不支持**跨族对比（例如 PostgreSQL ↔ MySQL）。前端选定源端后，目标端实例列表会按源端类型自动筛选；实例类型取自实例配置中的 `db_type`，无需单独选择「实例类型」。

PostgreSQL 可填写 Schema（默认 `public`）；MySQL 族使用实例当前库（`DATABASE()`），无 Schema 输入。

## 对比范围

### 共用（两族）

- 表是否存在（仅源 / 仅目标 / 两侧都有）
- 列：名称、类型、可空、默认值等（引擎语义各自采集）
- 表注释、列注释
- 主键 / UNIQUE / CHECK（MySQL CHECK 依赖 8.0.16+；不支持则跳过）
- 索引（与约束背后的索引去重）
- 外键
- 触发器
- Schema / 库内用户函数或例程（虚拟结果行）

### 仅 PostgreSQL

- 列 IDENTITY
- 序列默认值（`nextval`）
- RLS 开关（ENABLE / FORCE）与 Policy

### MySQL 族不含

- RLS / Policy（产品明确不做）

单次对比表数量上限 **200**（未选手动表名时，优先取同名表交集，再纳入单侧表）。

## 修复 SQL 原则

- 方向：**源端（左）→ 目标端（右）**
- **只增不删**：目标多出的对象只报告，不生成 `DROP`
- 定义「不一致」（如改列类型、改索引定义）默认输出 `-- TODO`，不自动 `MODIFY` / 盲删重建（与实现对齐）
- PostgreSQL 缺表：拼装 `CREATE TABLE` + 序列/约束/索引/注释/RLS/触发器（及依赖函数）
- MySQL 缺表：优先 `SHOW CREATE TABLE`，并附带触发器 DDL
- 差异表：按差异项生成 `ADD COLUMN` / `ADD CONSTRAINT` / `ADD INDEX` / `COMMENT` / `CREATE TRIGGER` / 序列与默认值 / RLS 等

任务完成后可在详情中预览、复制，并导出 **CSV / JSON / SQL**。

## 页面使用

1. 选择**源端**实例与库（PG 再选 Schema）。
2. 选择**目标端**（仅显示与源端同族的实例）。
3. 可选多选对比表；不选则自动取两侧同名表（上限 200）。
4. 点击「开始对比」，创建异步任务并轮询进度。
5. 在任务列表查看汇总（一致 / 差异 / 仅源 / 仅目标、是否有修复 SQL），点「查看」看明细与修复脚本。

菜单权限 URN：`um.sidebar.ops-migration-compare`。

## API（需登录）

前缀：`/api/v1/migration`

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/schema-compare/tasks` | 创建任务（兼容旧路径 `POST /schema-compare`） |
| `GET` | `/schema-compare/tasks` | 任务列表 |
| `GET` | `/schema-compare/tasks/:id` | 任务详情（含 `items`、`fix_sql`） |
| `GET` | `/schema-compare/tasks/:id/export?format=csv\|json\|sql` | 导出 |
| `DELETE` | `/schema-compare/tasks/:id` | 删除（执行中不可删） |

创建请求体示例：

```json
{
  "left_instance_id": "...",
  "right_instance_id": "...",
  "left_db": "db_src",
  "right_db": "db_tgt",
  "left_schema": "public",
  "right_schema": "public",
  "tables": ["tbl_a", "tbl_b"]
}
```

`tables` 可为空。MySQL 族可忽略 schema 字段。

## 实现位置

| 层 | 路径 |
|----|------|
| 前端 | `frontend/src/views/ops/MigrationSchemaCompare.tsx` |
| HTTP | `internal/handler/schema_compare.go` |
| 异步任务 | `internal/dbmig/schema_compare_runner.go` |
| 引擎适配 | `internal/dbmig/schema_engine.go`、`schema_engine_pg.go`、`schema_engine_mysql.go` |
| 对比 / 修复 | `schema_compare*.go`、`schema_fix_sql.go` |
| 落库 | `tbl_schema_compare_tasks`（含 `fix_sql`） |

## 自测脚本

在能访问对应数据库的环境执行：

```bash
# PostgreSQL（默认使用仓库配置可连的库；可用 SELFTEST_DSN 覆盖）
go run ./scripts/schema_compare_selftest.go all      # empty + differ
go run ./scripts/schema_compare_selftest.go empty
go run ./scripts/schema_compare_selftest.go differ

# MySQL 族（推荐双库）
SELFTEST_MYSQL_DSN='user:pass@tcp(host:3306)/srcdb?parseTime=true&multiStatements=true' \
SELFTEST_MYSQL_DSN_TGT='user:pass@tcp(host:3306)/tgtdb?parseTime=true&multiStatements=true' \
  go run ./scripts/schema_compare_selftest_mysql.go
```

仅设置 `SELFTEST_MYSQL_DSN` 时为单库模式，只校验修复 SQL 生成。

单元测试：

```bash
go test ./internal/dbmig/ -count=1
```
