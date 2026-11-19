package archive

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func deleteShard(ctx context.Context, db *sql.DB, table, cond string, pks []string, on func(Progress)) error {
	if len(pks) == 0 {
		return fmt.Errorf("分片删除需要主键")
	}
	if !hasCreateTime(ctx, db, "postgres", table) {
		return fmt.Errorf("分片删除需要表含 create_time 字段，请改用循环删除(cte)或自动")
	}
	hours, err := shardHours(ctx, db, table, cond)
	if err != nil {
		return err
	}
	bounds := SplitShardBounds(hours, shardThreshold)
	if len(bounds) == 0 {
		report(on, Progress{Message: "没有符合条件的数据"})
		return nil
	}
	dir, err := os.MkdirTemp("", "auto-dba-archive-shard-*")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(dir)

	var rounds int
	var deleted int64
	var prev *time.Time
	for i, bound := range bounds {
		if rounds >= maxRounds {
			return fmt.Errorf("超过最大轮次 %d", maxRounds)
		}
		path := filepath.Join(dir, fmt.Sprintf("shard_%04d.csv", i))
		nExport, err := exportShardPKs(ctx, db, table, cond, pks, prev, &bound, path)
		ct := bound
		prev = &ct
		if err != nil {
			return err
		}
		if nExport == 0 {
			_ = os.Remove(path)
			continue
		}
		n, batchRounds, lastSQL, err := deleteFromPKFile(ctx, db, table, pks, path, pgDeleteBatch, &rounds, &deleted, on)
		_ = n
		if err != nil {
			report(on, Progress{Rounds: rounds, DeletedRows: deleted, SQL: lastSQL})
			return err
		}
		_ = batchRounds
	}
	report(on, Progress{
		Rounds: rounds, DeletedRows: deleted,
		Message: fmt.Sprintf("分片删除完成，删除 %d 行 / %d 轮", deleted, rounds),
	})
	return nil
}

func shardHours(ctx context.Context, db *sql.DB, table, cond string) ([]HourCount, error) {
	q := fmt.Sprintf(`
SELECT date_trunc('hour', create_time) AS hour_start, count(*) AS data_count
FROM %s
WHERE %s AND create_time IS NOT NULL
GROUP BY hour_start
ORDER BY hour_start`, qualifyTable("postgres", table), normalizeCondition(cond))
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("按小时统计失败: %w", err)
	}
	defer rows.Close()
	var out []HourCount
	for rows.Next() {
		var h HourCount
		if err := rows.Scan(&h.Hour, &h.Count); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func exportShardPKs(ctx context.Context, db *sql.DB, table, cond string, pks []string, prev, curr *time.Time, path string) (int64, error) {
	qt := qualifyTable("postgres", table)
	quoted := make([]string, 0, len(pks))
	for _, pk := range pks {
		quoted = append(quoted, quoteIdent("postgres", pk))
	}
	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(quoted, ", "))
	b.WriteString(" FROM ")
	b.WriteString(qt)
	b.WriteString(" WHERE ")
	b.WriteString(normalizeCondition(cond))
	args := make([]any, 0, 2)
	nArg := 1
	if prev != nil {
		b.WriteString(fmt.Sprintf(" AND create_time >= $%d", nArg))
		args = append(args, *prev)
		nArg++
	}
	if curr != nil {
		b.WriteString(fmt.Sprintf(" AND create_time < $%d", nArg))
		args = append(args, *curr)
	}
	rows, err := db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("导出主键失败: %w", err)
	}
	defer rows.Close()
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	var n int64
	vals := make([]any, len(pks))
	ptrs := make([]any, len(pks))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return n, errStopped
		}
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		rec := make([]string, len(pks))
		for i, v := range vals {
			rec[i] = formatCSVValue(v)
		}
		if err := w.Write(rec); err != nil {
			return n, err
		}
		n++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return n, err
	}
	return n, rows.Err()
}

func formatCSVValue(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(t)
	}
}

func deleteFromPKFile(ctx context.Context, db *sql.DB, table string, pks []string, path string, batchSize int, rounds *int, deleted *int64, on func(Progress)) (int64, int, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, "", err
	}
	defer f.Close()
	r := csv.NewReader(bufio.NewReader(f))
	r.FieldsPerRecord = len(pks)
	r.TrimLeadingSpace = true
	if batchSize <= 0 {
		batchSize = pgDeleteBatch
	}
	var (
		total   int64
		batches int
		batch   [][]string
		lastSQL string
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if rounds != nil && *rounds >= maxRounds {
			return fmt.Errorf("超过最大轮次 %d", maxRounds)
		}
		sqlText, err := BuildPGDeleteByPKsSQL(table, pks, batch)
		if err != nil {
			return err
		}
		lastSQL = sqlText
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, sqlText)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		n, _ := res.RowsAffected()
		if err := tx.Commit(); err != nil {
			return err
		}
		total += n
		batches++
		if rounds != nil {
			*rounds++
		}
		if deleted != nil {
			*deleted += n
		}
		batch = batch[:0]
		shown := 0
		if rounds != nil {
			shown = *rounds
		}
		var rows int64
		if deleted != nil {
			rows = *deleted
		}
		report(on, interim(shown, rows, sqlText))
		return pauseBatch(ctx)
	}
	for {
		if err := ctx.Err(); err != nil {
			return total, batches, lastSQL, errStopped
		}
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, batches, lastSQL, err
		}
		batch = append(batch, append([]string(nil), rec...))
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return total, batches, lastSQL, err
			}
		}
	}
	if err := flush(); err != nil {
		return total, batches, lastSQL, err
	}
	return total, batches, lastSQL, nil
}
