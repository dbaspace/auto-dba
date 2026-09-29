package ticketexec

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CleanupRecordPrefix 自动清理无效索引写入的明细 SQL 前缀（不计入成功条数）。
const CleanupRecordPrefix = "[自动清理无效索引] "

const cicIndexStateSQL = `SELECT i.indisvalid,
	EXISTS (SELECT 1 FROM pg_stat_progress_create_index p WHERE p.index_relid = i.indexrelid)
FROM pg_index i WHERE i.indexrelid = to_regclass($1)`

// cleanupInvalidCICIndex CREATE INDEX CONCURRENTLY 被取消后会留下 indisvalid=false 的同名索引，
// IF NOT EXISTS 会把它当作已存在而跳过；执行前先删掉（其他会话仍在构建时报错，不删）。
func cleanupInvalidCICIndex(ctx context.Context, db *sql.DB, t Target, stmt string, collect *Result) error {
	name := parseCICIndexName(stmt)
	if name == "" {
		return nil
	}
	var valid, building bool
	err := db.QueryRowContext(ctx, cicIndexStateSQL, name).Scan(&valid, &building)
	// 无同名索引；或查询失败（PG 12 以下没有 pg_stat_progress_create_index）时不清理，按原语句执行
	if err != nil || valid {
		return nil
	}
	if building {
		return fmt.Errorf("索引 %s 正在由其他会话创建，请稍后重试", name)
	}
	drop := "DROP INDEX CONCURRENTLY IF EXISTS " + name
	started := time.Now()
	if _, err := db.ExecContext(ctx, drop); err != nil {
		return fmt.Errorf("清理无效索引 %s 失败: %w", name, err)
	}
	collect.appendRecord(Record{
		Host: t.Host, DB: t.DBName, Port: t.Port, Username: t.Username,
		SQL: CleanupRecordPrefix + drop, Status: "success",
		StartedAt: started.UTC().Format(time.RFC3339), EndedAt: nowRFC3339(),
	})
	return nil
}
