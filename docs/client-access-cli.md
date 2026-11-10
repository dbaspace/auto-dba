# 用 PAT 从 CLI 查询

管理员开通授权之后，用户自己签发个人访问令牌（PAT），CLI 用这枚令牌调用平台。登录 JWT 不能代替 PAT。浏览器登录不受影响。

令牌以 `adba_pat_` 开头，只在签发成功时显示一次。平台用它识别用户，并按该用户在当前租户里已经能看见的库执行查询。CLI 拿不到平台登记的主机账号和密码。

## 1. 管理员开通授权

入口：用户权限 → 客户端访问授权 → 访问授权。

1. 点「开通授权」。
2. 从平台用户列表选择对方的登录名。
3. 填写来源地址，每行一个。可以是公网 IP、内网 IP 或网段，例如 `61.49.49.50`、`10.8.0.12`、`10.0.0.0/8`。不填则该用户的 PAT 不能调用。
4. 有效天数默认 90。填 0 表示不过期。
5. 确认。范围固定为 `query`。

已开通的授权可以改来源地址，立即作用于该用户已经签发的 PAT。来源为空时 PAT 会被拒绝。

开通成功后，让对方用自己的账号登录，到「我的访问令牌」签发。管理员不要把浏览器登录令牌发给对方。调用必须从登记的地址发出。平台前面有反向代理时，由管理员在全局参数 `client_access_trusted_proxies` 填写代理地址，否则只认连到平台的 TCP 对端。

吊销这条授权后，该用户名下的 PAT 立即失效。

## 2. 用户签发 CLI 令牌

入口：用户权限 → 我的访问令牌（`/config/my-access-tokens`）。

页面顶部应显示「已授权」。若是「尚未开通客户端访问授权」，回到上一步，不要继续签发。

1. 点「签发令牌」。
2. 名称自定，例如 `cli`。
3. 类型选 **cli**。
4. 有效天数按需要填写，0 表示不过期。
5. 点「签发」，立刻复制明文。关闭窗口后只能看到前缀，不能再看全文。

## 3. CLI 怎么提交

地址与浏览器打开平台时相同，路径从 `/api/v1` 开始。每次请求带这两项头：

```http
Authorization: Bearer adba_pat_替换成签发时复制的全文
X-Client-Type: cli
```

`X-Client-Type` 请写 `cli`。签发时类型已经是 `cli` 时，不写这个头也会按 `cli` 记审计；写了则以请求头为准。

### 3.1 拉可见库

```bash
curl -sS "$BASE/api/v1/query/catalog" \
  -H "Authorization: Bearer $PAT" \
  -H "X-Client-Type: cli"
```

返回 `data.databases[]` 与 `data.instances[]`。查询时用库的 `id`，不是库名。目录不含主机、账号和密码。

### 3.2 看表名（可选）

```bash
curl -sS "$BASE/api/v1/databases/${DB_ID}/tables" \
  -H "Authorization: Bearer $PAT" \
  -H "X-Client-Type: cli"
```

### 3.3 执行查询

```bash
curl -sS "$BASE/api/v1/query" \
  -H "Authorization: Bearer $PAT" \
  -H "X-Client-Type: cli" \
  -H "Content-Type: application/json" \
  -d '{"database_id":"'"$DB_ID"'","sql":"SELECT id FROM t LIMIT 100"}'
```

成功时 `data.columns` 是列名，`data.rows` 是行。一次最多 3 条语句，语句之间用分号分开。

`SELECT` 和 `WITH` 必须在语句末尾自带正数 `LIMIT`，例如 `LIMIT 100`、`LIMIT 100 OFFSET 20` 或 `LIMIT 20, 100`（逗号形式里后面的数字是条数）。只写在子查询里不算。`LIMIT 0` 会被拒绝。`EXPLAIN`、`DESC` 以及允许的 `SHOW` 不要求 `LIMIT`。请求体里的 `limit` 字段不能代替 SQL 末尾的 `LIMIT`。令牌只放在 `Authorization` 头里，不要放进 URL。

允许的语句是 `SELECT`、`WITH`、`EXPLAIN`、`DESC`、`DESCRIBE`，以及 `SHOW TABLES`、`SHOW DATABASES`、`SHOW SCHEMAS`、`SHOW COLUMNS`、`SHOW FIELDS`、`SHOW CREATE TABLE`、`SHOW CREATE VIEW`、`SHOW INDEX`、`SHOW KEYS`。语句里不能出现 `pg_authid`、`pg_shadow`、`mysql.user`、`pg_read_file`、`LOAD_FILE`、`dblink`，也不能查询 `tbl_users`、`tbl_instances`、`tbl_db_accounts`、`tbl_client_pats`、`tbl_global_args`、`tbl_nacos_admin`、`tbl_oss_settings`。`SELECT` 正文里再出现 `INSERT`、`UPDATE`、`DELETE`、`MERGE`、`TRUNCATE`、`CREATE`、`DROP`、`ALTER`、`GRANT`、`COPY`、`INTO` 会被拒绝。`SHOW` 里出现 `INTO`、`OUTFILE`、`DUMPFILE` 会被拒绝。PostgreSQL、MySQL、TiDB 还会强制会话只读。

目标库是 StarRocks 时，除下一句列出的语句外，其余 `SHOW` 都可以查询，包括 `SHOW SESSION VARIABLES`、`SHOW GLOBAL VARIABLES`、`SHOW STATS META`、`SHOW HISTOGRAM META`。`ADMIN SHOW` 也可以查询，例如 `ADMIN SHOW CONFIG`、`ADMIN SHOW REPLICA STATUS`。`ADMIN SET` 会改配置，仍然拒绝。其它引擎执行这些 StarRocks 语句会返回「仅 StarRocks 可以执行该 SHOW」。

`SHOW GRANTS`、`SHOW CREATE USER`、`SHOW USERS`、`SHOW ROLES`、`SHOW PROPERTY`、`SHOW AUTHENTICATION`、`SHOW REPOSITORIES`、`SHOW STORAGE VOLUMES`、`SHOW CREATE STORAGE VOLUME`、`SHOW WHITELIST`、`SHOW SQLBLACKLIST` 会被拒绝。

不能用 `multipart/form-data` 上传 `.js`、`.mjs`、`.sql`、`.txt`、`.sh`。工单、建库、对象存储、Nacos、平台用户、实例详情和登记账号都不接受 PAT。

提交的 SQL（含被拒绝的）记在配置中心「查询审计」，「客户端」为 `cli`，「令牌」为该 PAT。接口审计只记方法、路径和状态，不记 SQL 正文。

## 4. CLI 可以调用的接口

| 方法 | 路径 | 用途 |
|------|------|------|
| GET | `/api/v1/query/catalog` | 当前用户可见的库 |
| GET | `/api/v1/databases/:id/tables` | 该库的表名 |
| POST | `/api/v1/query` | 执行只读 SQL |
| POST | `/api/v1/query/table-info` | 按 SQL 里的第一张表取字段 |
| GET | `/api/v1/query/favorites`、`/history` | 该用户的收藏和历史 |
| POST | `/api/v1/instances/:id/redis/execute` | Redis 只读命令 |
| GET | `/api/v1/instances/:id/kafka/topics` | Kafka 主题 |
| GET | `/api/v1/instances/:id/kafka/topics/:topic/messages` | Kafka 消息 |

工单执行（`POST /api/v1/tickets/:id/execute`）、建库、改实例、查看账号明文都不在这份名单里。PAT 不能拿平台登记的密码。

## 5. 接口审计里的拒绝

配置中心 → 客户端访问授权 → 接口审计。同一条 `GET /api/v1/query/catalog` 按下面顺序被拒绝：

| 说明 | 含义 | 处理 |
|------|------|------|
| `no client access grant` | 该用户没有生效中的访问授权 | 管理员按第 1 步开通 |
| `source not allowed` | 这次请求的来源地址不在该授权的来源名单里，或名单还是空的 | 管理员在授权上补上调用方的公网或内网地址 |
| `machine channel requires PAT` | 带了 `X-Client-Type: cli`，但 `Authorization` 不是 PAT（常见是把浏览器登录 JWT 贴过来） | 换成 `adba_pat_` 开头的令牌 |
| `scope missing` | 令牌没有 `query` 范围 | 吊销后重新签发；页面签发会跟随授权的 `query` |
| `path not in whitelist` | 授权和 PAT 已通过，但这个路径不在第 4 节 | 改调白名单里的路径。`/api/v1/query/catalog` 从 v1.12.97 起在白名单内，旧进程需要换到该版本后重启 |

查询被拒绝时 HTTP 400，正文 `message` 可直接给调用方。缺少末尾 `LIMIT` 时原文是：`PAT 查询的 SELECT 必须在语句末尾写 LIMIT，例如 LIMIT 100`。

## 6. 停用

- 用户在「我的访问令牌」吊销某一条，只失效这一条。
- 管理员在「客户端访问授权」吊销授权，该用户全部 PAT 失效。
