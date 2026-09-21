package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"auto-dba/internal/store"
)

func applySchema(ctx context.Context, engine string, src, tgt *sql.DB, o *store.MigrationObject, conflict string) error {
	exists, err := tableExists(ctx, tgt, engine, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	if err != nil {
		return err
	}
	if exists {
		switch conflict {
		case "skip":
			return nil
		case "drop_create":
			if err := dropTable(ctx, tgt, engine, o); err != nil {
				return err
			}
		case "truncate":
			return truncateTable(ctx, tgt, engine, o)
		default:
			return fmt.Errorf("目标表 %s 已存在（conflict=abort）", tableRef(engine, firstNonEmpty(o.TargetSchema, "public"), o.TargetTable))
		}
		if conflict == "truncate" {
			return nil
		}
	}
	if store.NormalizeDBType(engine) == "postgres" {
		return applyPGSchema(ctx, src, tgt, o)
	}
	return applyMySQLSchema(ctx, src, tgt, o)
}

func dropTable(ctx context.Context, tgt *sql.DB, engine string, o *store.MigrationObject) error {
	ref := tableRef(engine, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	_, err := tgt.ExecContext(ctx, "DROP TABLE IF EXISTS "+ref)
	return err
}

func truncateTable(ctx context.Context, tgt *sql.DB, engine string, o *store.MigrationObject) error {
	ref := tableRef(engine, firstNonEmpty(o.TargetSchema, o.SourceSchema), o.TargetTable)
	_, err := tgt.ExecContext(ctx, "TRUNCATE TABLE "+ref)
	return err
}

func applyMySQLSchema(ctx context.Context, src, tgt *sql.DB, o *store.MigrationObject) error {
	var tbl, ddl string
	err := src.QueryRowContext(ctx, "SHOW CREATE TABLE `"+strings.ReplaceAll(o.SourceTable, "`", "``")+"`").Scan(&tbl, &ddl)
	if err != nil {
		return fmt.Errorf("SHOW CREATE TABLE: %w", err)
	}
	ddl = strings.Replace(ddl, "CREATE TABLE `"+tbl+"`", "CREATE TABLE `"+strings.ReplaceAll(o.TargetTable, "`", "``")+"`", 1)
	_, err = tgt.ExecContext(ctx, ddl)
	return err
}

func applyPGSchema(ctx context.Context, src, tgt *sql.DB, o *store.MigrationObject) error {
	ss := firstNonEmpty(o.SourceSchema, "public")
	ts := firstNonEmpty(o.TargetSchema, ss)
	if _, err := tgt.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quoteIdent("postgres", ts)); err != nil {
		return err
	}
	rows, err := src.QueryContext(ctx, `
		SELECT column_name, data_type, udt_name, character_maximum_length, numeric_precision, numeric_scale,
		       is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema=$1 AND table_name=$2
		ORDER BY ordinal_position`, ss, o.SourceTable)
	if err != nil {
		return err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name, dtype, udt, nullable string
		var charLen, numPrec, numScale sql.NullInt64
		var def sql.NullString
		if err := rows.Scan(&name, &dtype, &udt, &charLen, &numPrec, &numScale, &nullable, &def); err != nil {
			return err
		}
		part := quoteIdent("postgres", name) + " " + pgColType(dtype, udt, charLen, numPrec, numScale)
		if nullable == "NO" {
			part += " NOT NULL"
		}
		if def.Valid && !strings.Contains(strings.ToLower(def.String), "nextval(") {
			part += " DEFAULT " + def.String
		}
		cols = append(cols, part)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(cols) == 0 {
		return fmt.Errorf("源表 %s.%s 无列", ss, o.SourceTable)
	}
	pk, _ := pgPrimaryKey(ctx, src, ss, o.SourceTable)
	if pk != "" {
		cols = append(cols, pk)
	}
	ddl := fmt.Sprintf(`CREATE TABLE %s (%s)`, tableRef("postgres", ts, o.TargetTable), strings.Join(cols, ", "))
	if _, err := tgt.ExecContext(ctx, ddl); err != nil {
		return err
	}
	return copyPGIndexes(ctx, src, tgt, ss, o.SourceTable, ts, o.TargetTable)
}

func pgColType(dtype, udt string, charLen, numPrec, numScale sql.NullInt64) string {
	switch strings.ToLower(dtype) {
	case "character varying", "varchar":
		if charLen.Valid && charLen.Int64 > 0 {
			return fmt.Sprintf("varchar(%d)", charLen.Int64)
		}
		return "varchar"
	case "character", "char":
		if charLen.Valid && charLen.Int64 > 0 {
			return fmt.Sprintf("char(%d)", charLen.Int64)
		}
		return "char"
	case "numeric", "decimal":
		if numPrec.Valid && numScale.Valid {
			return fmt.Sprintf("numeric(%d,%d)", numPrec.Int64, numScale.Int64)
		}
		return "numeric"
	case "array":
		return udt
	case "user-defined":
		return quoteIdent("postgres", udt)
	default:
		if udt != "" && dtype != udt {
			return udt
		}
		return dtype
	}
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

func copyPGIndexes(ctx context.Context, src, tgt *sql.DB, ss, st, ts, tt string) error {
	rows, err := src.QueryContext(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname=$1 AND tablename=$2 AND indexname NOT IN (
			SELECT constraint_name FROM information_schema.table_constraints
			WHERE table_schema=$1 AND table_name=$2 AND constraint_type='PRIMARY KEY'
		)`, ss, st)
	if err != nil {
		return err
	}
	defer rows.Close()
	srcOn := []string{
		`ON "` + ss + `"."` + st + `"`,
		`ON ` + ss + `."` + st + `"`,
		`ON "` + ss + `".` + st,
		`ON ` + ss + `.` + st,
	}
	tgtOn := `ON ` + tableRef("postgres", ts, tt)
	for rows.Next() {
		var def string
		if err := rows.Scan(&def); err != nil {
			return err
		}
		replaced := false
		for _, p := range srcOn {
			if strings.Contains(def, p) {
				def = strings.Replace(def, p, tgtOn, 1)
				replaced = true
				break
			}
		}
		if !replaced {
			// 无法安全改写（常见于 public."MixedCase"）则跳过，避免目标库误引用源表名
			continue
		}
		if _, err := tgt.ExecContext(ctx, def); err != nil {
			// 同名索引冲突时跳过，不阻断数据迁移
			if !strings.Contains(strings.ToLower(err.Error()), "already exists") {
				return err
			}
		}
	}
	return rows.Err()
}
