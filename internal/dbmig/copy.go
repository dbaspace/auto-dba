package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/store"
)

const maxBindParams = 65535

func copyTable(ctx context.Context, engine string, src, tgt *sql.DB, o *store.MigrationObject, batchSize, speedLimit int, onBatch func(n int64) error) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	cols, err := listColumns(ctx, src, engine, firstNonEmpty(o.SourceSchema, "public"), o.SourceTable)
	if err != nil {
		return 0, err
	}
	if len(cols) == 0 {
		return 0, fmt.Errorf("源表无列")
	}
	overriding := false
	if store.NormalizeDBType(engine) == "postgres" {
		var ierr error
		overriding, ierr = pgIdentityAlways(ctx, tgt, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
		if ierr != nil {
			return 0, ierr
		}
	}
	chunk := maxInsertRows(len(cols))
	if batchSize > chunk {
		batchSize = chunk
	}
	// 断点续传（已写入部分行）时走流式并跳过已确认行；完整表优先尝试 PG COPY。
	// GENERATED ALWAYS AS IDENTITY 不能用 COPY 写入显式值。
	if store.NormalizeDBType(engine) == "postgres" && o.CompletedRows == 0 && !overriding {
		if n, err := copyTablePostgres(ctx, src, tgt, o, cols); err == nil {
			if onBatch != nil && n > 0 {
				_ = onBatch(n)
			}
			return n, nil
		}
	}
	return copyTableStream(ctx, engine, src, tgt, o, cols, batchSize, speedLimit, overriding, onBatch)
}

// copyTableStream 单次流式 SELECT，禁止深 OFFSET 翻页。
func copyTableStream(ctx context.Context, engine string, src, tgt *sql.DB, o *store.MigrationObject, cols []string, batchSize, speedLimit int, overriding bool, onBatch func(n int64) error) (int64, error) {
	colSQL := joinQuoted(engine, cols)
	srcRef := tableRef(engine, firstNonEmpty(o.SourceSchema, "public"), o.SourceTable)
	skip := o.CompletedRows
	q := fmt.Sprintf("SELECT %s FROM %s", colSQL, srcRef)
	rows, err := src.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var total int64
	var skipped int64
	batch := make([][]any, 0, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := insertBatch(ctx, tgt, engine, o, cols, batch, overriding); err != nil {
			return err
		}
		n := int64(len(batch))
		total += n
		if onBatch != nil {
			if err := onBatch(n); err != nil {
				return err
			}
		}
		if speedLimit > 0 {
			d := time.Duration(float64(time.Second) * float64(n) / float64(speedLimit))
			if d > 0 {
				time.Sleep(d)
			}
		}
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return total, err
		}
		for i := range vals {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
		}
		if skip > 0 && skipped < skip {
			skipped++
			continue
		}
		batch = append(batch, vals)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total + skip, nil
}

func copyTablePostgres(ctx context.Context, src, tgt *sql.DB, o *store.MigrationObject, cols []string) (int64, error) {
	srcRef := tableRef("postgres", firstNonEmpty(o.SourceSchema, "public"), o.SourceTable)
	tgtRef := tableRef("postgres", firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	colSQL := joinQuoted("postgres", cols)
	copyOut := fmt.Sprintf("COPY (SELECT %s FROM %s) TO STDOUT WITH (FORMAT binary)", colSQL, srcRef)
	copyIn := fmt.Sprintf("COPY %s (%s) FROM STDIN WITH (FORMAT binary)", tgtRef, colSQL)

	srcConn, err := src.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer srcConn.Close()
	tgtConn, err := tgt.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer tgtConn.Close()

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		defer pw.Close()
		err := srcConn.Raw(func(driverConn any) error {
			sc, ok := driverConn.(*stdlib.Conn)
			if !ok || sc.Conn() == nil {
				return fmt.Errorf("非 pgx 连接")
			}
			_, err := sc.Conn().PgConn().CopyTo(ctx, pw, copyOut)
			return err
		})
		if err != nil {
			_ = pw.CloseWithError(err)
		}
		errCh <- err
	}()

	var n int64
	err = tgtConn.Raw(func(driverConn any) error {
		sc, ok := driverConn.(*stdlib.Conn)
		if !ok || sc.Conn() == nil {
			return fmt.Errorf("非 pgx 连接")
		}
		tag, err := sc.Conn().PgConn().CopyFrom(ctx, pr, copyIn)
		if err != nil {
			return err
		}
		n = tag.RowsAffected()
		return nil
	})
	_ = pr.Close()
	srcErr := <-errCh
	if err != nil {
		return 0, err
	}
	if srcErr != nil {
		return 0, srcErr
	}
	return n, nil
}

func listColumns(ctx context.Context, db *sql.DB, engine, schema, table string) ([]string, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if store.NormalizeDBType(engine) == "postgres" {
		rows, err = db.QueryContext(ctx, `
			SELECT a.attname
			FROM pg_attribute a
			JOIN pg_class cl ON a.attrelid = cl.oid
			JOIN pg_namespace n ON cl.relnamespace = n.oid
			WHERE n.nspname=$1 AND cl.relname=$2
				AND a.attnum > 0 AND NOT a.attisdropped
				AND COALESCE(a.attgenerated::text, '') = ''
			ORDER BY a.attnum`, schema, table)
	} else {
		rows, err = db.QueryContext(ctx, `
			SELECT COLUMN_NAME FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?
				AND EXTRA NOT LIKE '%VIRTUAL GENERATED%'
				AND EXTRA NOT LIKE '%STORED GENERATED%'
			ORDER BY ORDINAL_POSITION`, table)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func insertBatch(ctx context.Context, tgt *sql.DB, engine string, o *store.MigrationObject, cols []string, batch [][]any, overriding bool) error {
	if len(batch) == 0 {
		return nil
	}
	chunk := maxInsertRows(len(cols))
	for start := 0; start < len(batch); start += chunk {
		end := start + chunk
		if end > len(batch) {
			end = len(batch)
		}
		if err := insertChunk(ctx, tgt, engine, o, cols, batch[start:end], overriding); err != nil {
			return err
		}
	}
	return nil
}

func insertChunk(ctx context.Context, tgt *sql.DB, engine string, o *store.MigrationObject, cols []string, batch [][]any, overriding bool) error {
	ref := tableRef(engine, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	colSQL := joinQuoted(engine, cols)
	ph := "(" + strings.TrimRight(strings.Repeat("?,", len(cols)), ",") + ")"
	if store.NormalizeDBType(engine) == "postgres" {
		var b strings.Builder
		args := make([]any, 0, len(batch)*len(cols))
		n := 1
		for i, row := range batch {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteByte('(')
			for j := range cols {
				if j > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, "$%d", n)
				n++
			}
			b.WriteByte(')')
			args = append(args, row...)
		}
		q := pgInsertSQL(ref, colSQL, b.String(), overriding && store.NormalizeDBType(engine) == "postgres")
		_, err := tgt.ExecContext(ctx, q, args...)
		return err
	}
	values := make([]string, len(batch))
	args := make([]any, 0, len(batch)*len(cols))
	for i, row := range batch {
		values[i] = ph
		args = append(args, row...)
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", ref, colSQL, strings.Join(values, ","))
	_, err := tgt.ExecContext(ctx, q, args...)
	return err
}

func joinQuoted(engine string, cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = quoteIdent(engine, c)
	}
	return strings.Join(out, ",")
}

func maxInsertRows(colCount int) int {
	if colCount <= 0 {
		return 1
	}
	n := maxBindParams / colCount
	if n < 1 {
		return 1
	}
	return n
}
