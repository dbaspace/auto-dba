package pgspace

// 表空间/存储分析采集 SQL。
// 全部为只读元数据查询（读取系统目录与统计视图，不触发数据页全表扫描）；
// 分库分表场景在服务端按归一化逻辑名聚合，避免把单库 4096 张分片表逐行拉回应用层。

// pgSpaceTablespaceSQL 集群级表空间用量。pg_tablespace_size 需 pg_read_all_stats/超级用户权限，
// 无权限时整条查询会失败，调用方按「失败即降级」处理（记警告、跳过该维度）。
const pgSpaceTablespaceSQL = `
SELECT spcname AS name,
       pg_tablespace_size(oid) AS size_bytes
FROM pg_tablespace
ORDER BY size_bytes DESC`

// pgSpaceDatabaseSQL 库级用量 + 冻结事务年龄。
const pgSpaceDatabaseSQL = `
SELECT d.datname AS name,
       pg_database_size(d.datname) AS size_bytes,
       COALESCE(t.spcname, '') AS tablespace_name,
       age(d.datfrozenxid)::bigint AS datfrozenxid_age
FROM pg_database d
LEFT JOIN pg_tablespace t ON t.oid = d.dattablespace
WHERE NOT d.datistemplate
  AND d.datallowconn
  AND has_database_privilege(current_user, d.datname, 'CONNECT')
ORDER BY size_bytes DESC`

// pgSpaceTablesSQL 大表用量（分片聚合）。$1=分片剥离正则，$2=Top-N。
// 含访问/缓存/热点 DML / VACUUM·ANALYZE 时间 / XID 年龄；膨胀仍在应用层估算。
const pgSpaceTablesSQL = `
SELECT n.nspname AS schema_name,
       regexp_replace(c.relname, $1, '') AS logical_name,
       COUNT(*) AS shard_count,
       SUM(pg_total_relation_size(c.oid))::bigint AS total_bytes,
       SUM(pg_relation_size(c.oid))::bigint AS table_bytes,
       SUM(pg_indexes_size(c.oid))::bigint AS index_bytes,
       SUM(COALESCE(pg_total_relation_size(c.reltoastrelid), 0))::bigint AS toast_bytes,
       SUM(COALESCE(st.n_live_tup, 0))::bigint AS n_live_tup,
       SUM(COALESCE(st.n_dead_tup, 0))::bigint AS n_dead_tup,
       SUM(COALESCE(st.seq_scan, 0))::bigint AS seq_scan,
       SUM(COALESCE(st.idx_scan, 0))::bigint AS tbl_idx_scan,
       SUM(COALESCE(st.heap_blks_read, 0))::bigint AS heap_blks_read,
       SUM(COALESCE(st.heap_blks_hit, 0))::bigint AS heap_blks_hit,
       SUM(COALESCE(st.n_tup_ins, 0))::bigint AS n_tup_ins,
       SUM(COALESCE(st.n_tup_upd, 0))::bigint AS n_tup_upd,
       SUM(COALESCE(st.n_tup_del, 0))::bigint AS n_tup_del,
       MIN(st.last_vacuum) AS last_vacuum,
       MIN(st.last_autovacuum) AS last_autovacuum,
       MIN(st.last_analyze) AS last_analyze,
       MIN(st.last_autoanalyze) AS last_autoanalyze,
       MAX(age(c.relfrozenxid))::bigint AS relfrozenxid_age
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_stat_user_tables st ON st.relid = c.oid
WHERE c.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname !~ '^pg_'
GROUP BY schema_name, logical_name
ORDER BY total_bytes DESC
LIMIT $2`

// pgSpaceHotTablesSQL 按累计 DML 取热点逻辑表（与 size Top-N 互补）。$1=正则，$2=Top-N。
const pgSpaceHotTablesSQL = `
SELECT n.nspname AS schema_name,
       regexp_replace(c.relname, $1, '') AS logical_name,
       COUNT(*) AS shard_count,
       SUM(pg_total_relation_size(c.oid))::bigint AS total_bytes,
       SUM(pg_relation_size(c.oid))::bigint AS table_bytes,
       SUM(pg_indexes_size(c.oid))::bigint AS index_bytes,
       SUM(COALESCE(pg_total_relation_size(c.reltoastrelid), 0))::bigint AS toast_bytes,
       SUM(COALESCE(st.n_live_tup, 0))::bigint AS n_live_tup,
       SUM(COALESCE(st.n_dead_tup, 0))::bigint AS n_dead_tup,
       SUM(COALESCE(st.seq_scan, 0))::bigint AS seq_scan,
       SUM(COALESCE(st.idx_scan, 0))::bigint AS tbl_idx_scan,
       SUM(COALESCE(st.heap_blks_read, 0))::bigint AS heap_blks_read,
       SUM(COALESCE(st.heap_blks_hit, 0))::bigint AS heap_blks_hit,
       SUM(COALESCE(st.n_tup_ins, 0))::bigint AS n_tup_ins,
       SUM(COALESCE(st.n_tup_upd, 0))::bigint AS n_tup_upd,
       SUM(COALESCE(st.n_tup_del, 0))::bigint AS n_tup_del,
       MIN(st.last_vacuum) AS last_vacuum,
       MIN(st.last_autovacuum) AS last_autovacuum,
       MIN(st.last_analyze) AS last_analyze,
       MIN(st.last_autoanalyze) AS last_autoanalyze,
       MAX(age(c.relfrozenxid))::bigint AS relfrozenxid_age
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_stat_user_tables st ON st.relid = c.oid
WHERE c.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname !~ '^pg_'
GROUP BY schema_name, logical_name
ORDER BY (SUM(COALESCE(st.n_tup_ins,0))+SUM(COALESCE(st.n_tup_upd,0))+SUM(COALESCE(st.n_tup_del,0))) DESC
LIMIT $2`

// pgSpaceSequencesSQL 序列用量与耗尽风险（分片聚合）。$1=正则，$2=Top-N。
// 取分片中最大 used_ratio / 最小 remaining 代表逻辑序列风险。
const pgSpaceSequencesSQL = `
SELECT schemaname AS schema_name,
       regexp_replace(sequencename, $1, '') AS logical_name,
       COUNT(*) AS shard_count,
       MAX(COALESCE(last_value, 0))::bigint AS seq_last_value,
       MAX(COALESCE(max_value, 0))::bigint AS seq_max_value,
       MIN(COALESCE(min_value, 0))::bigint AS seq_min_value,
       BOOL_OR(COALESCE(cycle, false)) AS seq_cycle,
       MAX(
         CASE
           WHEN COALESCE(max_value, 0) > COALESCE(min_value, 0)
             THEN (COALESCE(last_value, 0)::numeric - min_value::numeric)
                  / (max_value::numeric - min_value::numeric)
           ELSE 0
         END
       )::double precision AS seq_used_ratio,
       MIN(GREATEST(COALESCE(max_value, 0) - COALESCE(last_value, 0), 0))::bigint AS seq_remaining
FROM pg_sequences
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
  AND schemaname !~ '^pg_'
GROUP BY schema_name, logical_name
ORDER BY seq_used_ratio DESC NULLS LAST, seq_remaining ASC NULLS LAST
LIMIT $2`

// pgSpaceIndexesSQL 大索引用量（分片聚合）。$1=分片剥离正则，$2=Top-N。
const pgSpaceIndexesSQL = `
SELECT s.schemaname AS schema_name,
       regexp_replace(s.indexrelname, $1, '') AS logical_name,
       COUNT(*) AS shard_count,
       SUM(pg_relation_size(s.indexrelid))::bigint AS size_bytes,
       SUM(COALESCE(s.idx_scan, 0))::bigint AS idx_scan
FROM pg_stat_user_indexes s
GROUP BY schema_name, logical_name
ORDER BY size_bytes DESC
LIMIT $2`

// pgSpaceUnusedIndexesSQL 无用索引（分片聚合）：$1=分片剥离正则，$2=Top-N。
const pgSpaceUnusedIndexesSQL = `
SELECT s.schemaname AS schema_name,
       regexp_replace(s.indexrelname, $1, '') AS logical_name,
       COUNT(*) AS shard_count,
       SUM(pg_relation_size(s.indexrelid))::bigint AS size_bytes,
       SUM(COALESCE(s.idx_scan, 0))::bigint AS idx_scan
FROM pg_stat_user_indexes s
JOIN pg_index i ON i.indexrelid = s.indexrelid
WHERE NOT i.indisunique
  AND NOT i.indisprimary
GROUP BY schema_name, logical_name
HAVING SUM(COALESCE(s.idx_scan, 0)) = 0
ORDER BY size_bytes DESC
LIMIT $2`
