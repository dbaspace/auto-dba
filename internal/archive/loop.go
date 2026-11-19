package archive

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Progress 是一轮归档的进度。Interim 为真时文案只表示还在跑。
type Progress struct {
	Rounds      int
	DeletedRows int64
	SQL         string
	Message     string
	Interim     bool
}

func report(on func(Progress), p Progress) {
	if on != nil {
		on(p)
	}
}

func interim(rounds int, rows int64, sql string) Progress {
	return Progress{
		Rounds: rounds, DeletedRows: rows, SQL: sql, Interim: true,
		Message: fmt.Sprintf("执行中：第 %d 轮，已删除 %d 行", rounds, rows),
	}
}

// RunOnDB 在已经打开的连接上按策略执行。拷贝失败时不会进入删除。
func RunOnDB(ctx context.Context, strategy string, src, dst *sql.DB, dbType, table, condition, pgMode, scriptType, ossBucket, dstTable string, cleanupAfter bool, on func(Progress)) error {
	switch NormalizeStrategy(strategy) {
	case "custom_script":
		return runScript(ctx, src, scriptType, condition, on)
	case "backup_target":
		if dst == nil {
			return fmt.Errorf("备份到目标实例须指定目标库")
		}
		if strings.TrimSpace(dstTable) == "" {
			dstTable = table
		}
		return copyThenDelete(ctx, src, dst, dbType, table, condition, pgMode, dstTable, on)
	case "archive_oss":
		if err := exportCSV(ctx, src, dbType, table, condition, ossBucket, on); err != nil {
			return err
		}
		if cleanupAfter {
			return deleteLoop(ctx, src, dbType, table, condition, pgMode, on)
		}
		return nil
	default:
		return deleteLoop(ctx, src, dbType, table, condition, pgMode, on)
	}
}

func deleteLoop(ctx context.Context, db *sql.DB, dbType, table, cond, pgMode string, on func(Progress)) error {
	if err := ValidateDeleteCondition(cond); err != nil {
		return err
	}
	pks, err := tablePKs(ctx, db, dbType, table)
	if err != nil {
		return fmt.Errorf("读取主键失败: %w", err)
	}
	mode := "cte"
	if dbType == "postgres" {
		hasCT := hasCreateTime(ctx, db, dbType, table)
		var rerr error
		mode, rerr = ResolvePGMode(pgMode, hasCT, estimatePGRows(ctx, db, table))
		if rerr != nil {
			return rerr
		}
		if mode == "shard" {
			return deleteShard(ctx, db, table, cond, pks, on)
		}
	}
	var rounds int
	var deleted int64
	var lastSQL string
	for rounds < maxRounds {
		if err := ctx.Err(); err != nil {
			return errStopped
		}
		var q string
		var n int64
		if dbType == "postgres" {
			q, err = BuildPGCTESQL(table, cond, pks)
			if err != nil {
				return err
			}
			lastSQL = q
			n, err = execPGCTEBatch(ctx, db, q)
		} else {
			q, err = BuildMySQLDeleteSQL(table, cond)
			if err != nil {
				return err
			}
			lastSQL = q
			n, err = execAffected(ctx, db, q)
		}
		if err != nil {
			report(on, Progress{Rounds: rounds, DeletedRows: deleted, SQL: lastSQL})
			return err
		}
		rounds++
		deleted += n
		report(on, interim(rounds, deleted, lastSQL))
		if n == 0 {
			return nil
		}
		if err := pauseBatch(ctx); err != nil {
			return err
		}
	}
	return fmt.Errorf("超过最大轮次 %d", maxRounds)
}

func execPGCTEBatch(ctx context.Context, db *sql.DB, q string) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return 0, err
	}
	var n int64
	for rows.Next() {
		holders := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range holders {
			ptrs[i] = &holders[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			rows.Close()
			return 0, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func pauseBatch(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return errStopped
	case <-time.After(600 * time.Millisecond):
		return nil
	}
}

func estimatePGRows(ctx context.Context, db *sql.DB, table string) int64 {
	schema, name := splitTable(table)
	if schema == "" {
		schema = "public"
	}
	var n sql.NullFloat64
	err := db.QueryRowContext(ctx, `
SELECT c.reltuples
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relname = $1 AND n.nspname = $2
LIMIT 1`, name, schema).Scan(&n)
	if err != nil || !n.Valid || n.Float64 < 0 {
		return 0
	}
	return int64(n.Float64)
}

func splitTable(table string) (schema, name string) {
	table = strings.TrimSpace(table)
	if i := strings.LastIndex(table, "."); i >= 0 {
		return strings.TrimSpace(table[:i]), strings.TrimSpace(table[i+1:])
	}
	return "", table
}

func copyThenDelete(ctx context.Context, src, dst *sql.DB, dbType, table, cond, pgMode, dstTable string, on func(Progress)) error {
	if err := ValidateDeleteCondition(cond); err != nil {
		return err
	}
	sel := fmt.Sprintf("SELECT * FROM %s WHERE %s", qualifyTable(dbType, table), normalizeCondition(cond))
	rows, err := src.QueryContext(ctx, sel)
	if err != nil {
		return fmt.Errorf("读取源表失败: %w", err)
	}
	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return err
	}
	qCols := make([]string, len(cols))
	ph := make([]string, len(cols))
	for i, c := range cols {
		qCols[i] = quoteIdent(dbType, c)
		ph[i] = "?"
		if dbType == "postgres" {
			ph[i] = fmt.Sprintf("$%d", i+1)
		}
	}
	ins := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", qualifyTable(dbType, dstTable), strings.Join(qCols, ","), strings.Join(ph, ","))
	copied := int64(0)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			rows.Close()
			return errStopped
		}
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			rows.Close()
			return err
		}
		if _, err := dst.ExecContext(ctx, ins, raw...); err != nil {
			rows.Close()
			return fmt.Errorf("写入目标表失败: %w", err)
		}
		copied++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	report(on, Progress{
		SQL: sel, Message: fmt.Sprintf("已备份 %d 行到 %s", copied, dstTable),
	})
	return deleteLoop(ctx, src, dbType, table, cond, pgMode, on)
}

func exportCSV(ctx context.Context, src *sql.DB, dbType, table, cond, ossBucket string, on func(Progress)) error {
	if err := ValidateDeleteCondition(cond); err != nil {
		return err
	}
	sel := fmt.Sprintf("SELECT * FROM %s WHERE %s", qualifyTable(dbType, table), normalizeCondition(cond))
	rows, err := src.QueryContext(ctx, sel)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	dir := filepath.Join(os.TempDir(), "auto-dba-archive")
	_ = os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("%s_%s.csv", strings.ReplaceAll(table, ".", "_"), time.Now().UTC().Format("20060102T150405"))
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write(cols)
	n := int64(0)
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			f.Close()
			return err
		}
		rec := make([]string, len(cols))
		for i, v := range raw {
			rec[i] = fmt.Sprint(v)
		}
		_ = w.Write(rec)
		n++
	}
	w.Flush()
	f.Close()
	msg := fmt.Sprintf("已导出 %d 行到 %s", n, path)
	if strings.TrimSpace(ossBucket) != "" {
		msg += "（OSS bucket=" + ossBucket + "，请在系统设置中配置上传）"
	}
	report(on, Progress{SQL: sel, Message: msg})
	return nil
}
