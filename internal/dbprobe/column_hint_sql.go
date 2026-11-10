package dbprobe

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var errHintTable = errors.New("表名不能为空")

func listMySQLHintColumns(ctx context.Context, host string, port int, user, password, dbName, table string) ([]HintColumn, error) {
	db, err := openMySQL(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, IFNULL(COLUMN_COMMENT, '')
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, dbName, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]HintColumn, 0)
	for rows.Next() {
		var name, typ, nullable, comment string
		if err := rows.Scan(&name, &typ, &nullable, &comment); err != nil {
			return nil, err
		}
		out = append(out, HintColumn{
			Name:     name,
			DataType: typ,
			Nullable: strings.EqualFold(nullable, "YES"),
			Comment:  strings.TrimSpace(comment),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func listPostgresHintColumns(ctx context.Context, host string, port int, user, password, dbName, schema, table string) ([]HintColumn, error) {
	db, err := openPostgres(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT a.attname,
			pg_catalog.format_type(a.atttypid, a.atttypmod),
			NOT a.attnotnull,
			COALESCE(col_description(a.attrelid, a.attnum), '')
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
			AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPGHintColumns(rows)
}

func scanPGHintColumns(rows *sql.Rows) ([]HintColumn, error) {
	out := make([]HintColumn, 0)
	for rows.Next() {
		var name, typ, comment string
		var nullable bool
		if err := rows.Scan(&name, &typ, &nullable, &comment); err != nil {
			return nil, err
		}
		out = append(out, HintColumn{
			Name:     name,
			DataType: typ,
			Nullable: nullable,
			Comment:  strings.TrimSpace(comment),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
