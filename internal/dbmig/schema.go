package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

func applySchema(ctx context.Context, engine string, src, tgt *sql.DB, o *store.MigrationObject, conflict string, pairs []pgTablePair) (*schemaDeferred, error) {
	exists, err := tableExists(ctx, tgt, engine, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	if err != nil {
		return nil, err
	}
	if exists {
		switch conflict {
		case "skip":
			return &schemaDeferred{}, nil
		case "drop_create":
			if err := dropTable(ctx, tgt, engine, o); err != nil {
				return nil, err
			}
		case "truncate":
			if err := truncateTable(ctx, tgt, engine, o); err != nil {
				return nil, err
			}
			return &schemaDeferred{}, nil
		default:
			return nil, fmt.Errorf("目标表 %s 已存在（conflict=abort）", tableRef(engine, firstNonEmpty(o.TargetSchema, "public"), o.TargetTable))
		}
	}
	if store.NormalizeDBType(engine) == "postgres" {
		return applyPGSchema(ctx, src, tgt, o, pairs)
	}
	return applyMySQLSchema(ctx, src, tgt, o, pairs)
}

func dropTable(ctx context.Context, tgt *sql.DB, engine string, o *store.MigrationObject) error {
	ref := tableRef(engine, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	q := "DROP TABLE IF EXISTS " + ref
	if store.NormalizeDBType(engine) == "postgres" {
		q += " CASCADE"
	}
	_, err := tgt.ExecContext(ctx, q)
	return err
}

func truncateTable(ctx context.Context, tgt *sql.DB, engine string, o *store.MigrationObject) error {
	ref := tableRef(engine, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	_, err := tgt.ExecContext(ctx, "TRUNCATE TABLE "+ref)
	return err
}

func applyMySQLSchema(ctx context.Context, src, tgt *sql.DB, o *store.MigrationObject, pairs []pgTablePair) (*schemaDeferred, error) {
	var tbl, ddl string
	err := src.QueryRowContext(ctx, "SHOW CREATE TABLE `"+strings.ReplaceAll(o.SourceTable, "`", "``")+"`").Scan(&tbl, &ddl)
	if err != nil {
		return nil, fmt.Errorf("SHOW CREATE TABLE: %w", err)
	}
	ddl = strings.Replace(ddl, "CREATE TABLE `"+tbl+"`", "CREATE TABLE `"+strings.ReplaceAll(o.TargetTable, "`", "``")+"`", 1)
	body, fks := splitMySQLForeignKeys(ddl, o.TargetTable, pairs)
	if _, err := tgt.ExecContext(ctx, body); err != nil {
		return nil, fmt.Errorf("执行建表失败: %w\nSQL: %s", err, body)
	}
	trigs, err := mysqlTriggerDDLs(ctx, src, o.SourceTable, o.TargetTable)
	if err != nil {
		return nil, err
	}
	return &schemaDeferred{Created: true, FKs: fks, Triggers: trigs}, nil
}

func applyPGSchema(ctx context.Context, src, tgt *sql.DB, o *store.MigrationObject, pairs []pgTablePair) (*schemaDeferred, error) {
	ss := firstNonEmpty(o.SourceSchema, "public")
	ts := firstNonEmpty(o.TargetSchema, ss)
	plan, err := planPGTable(ctx, src, ss, o.SourceTable, ts, o.TargetTable, pairs)
	if err != nil {
		return nil, err
	}
	for _, stmt := range plan.Body {
		if err := execSQL(ctx, tgt, "结构迁移", stmt); err != nil {
			return nil, err
		}
	}
	return &schemaDeferred{Created: true, FKs: plan.FKs, Triggers: plan.Triggers, SeqSyncs: plan.SeqSyncs}, nil
}

func pgPrimaryKey(ctx context.Context, src *sql.DB, schema, table string) (string, error) {
	rows, err := src.QueryContext(ctx, `
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		WHERE tc.table_schema=$1 AND tc.table_name=$2 AND tc.constraint_type='PRIMARY KEY'
		ORDER BY kcu.ordinal_position`, schema, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return "", err
		}
		cols = append(cols, quoteIdent("postgres", c))
	}
	if len(cols) == 0 {
		return "", nil
	}
	return "PRIMARY KEY (" + strings.Join(cols, ", ") + ")", nil
}
