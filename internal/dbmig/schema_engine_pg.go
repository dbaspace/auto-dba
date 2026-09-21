package dbmig

import (
	"context"
	"database/sql"
	"fmt"
)

type schemaEnginePG struct{}

func (schemaEnginePG) FetchAndDiff(ctx context.Context, left, right *sql.DB, leftSchema, rightSchema, table string) (TableSchemaDiff, error) {
	ls, err := fetchPGTableSchemaInfo(ctx, left, leftSchema, table)
	if err != nil {
		return TableSchemaDiff{}, fmt.Errorf("读左侧结构: %w", err)
	}
	rs, err := fetchPGTableSchemaInfo(ctx, right, rightSchema, table)
	if err != nil {
		return TableSchemaDiff{}, fmt.Errorf("读右侧结构: %w", err)
	}
	diffs := diffPGTableSchema(ls, rs)
	if len(diffs) == 0 {
		return TableSchemaDiff{Table: table, Status: "match", Message: "结构一致"}, nil
	}
	return TableSchemaDiff{Table: table, Status: "differ", Message: fmt.Sprintf("%d 处差异", len(diffs)), Diffs: diffs}, nil
}

func (schemaEnginePG) DiffRoutines(ctx context.Context, left, right *sql.DB, leftSchema, rightSchema string) []TableSchemaDiff {
	return nil
}

func (schemaEnginePG) BuildCreateSQL(ctx context.Context, src *sql.DB, leftSchema, rightSchema, table string) (string, error) {
	return buildPGCreateTableSQL(ctx, src, leftSchema, rightSchema, table)
}

func (schemaEnginePG) BuildAlterSQL(ctx context.Context, src *sql.DB, leftSchema, rightSchema string, item *TableSchemaDiff) (string, error) {
	return buildPGAlterFixSQL(ctx, src, leftSchema, rightSchema, item)
}

func (schemaEnginePG) BuildRoutineFixSQL(ctx context.Context, src *sql.DB, leftSchema, rightSchema string, item *TableSchemaDiff) (string, error) {
	return buildPGFunctionFixSQL(ctx, src, leftSchema, rightSchema, item)
}

func (schemaEnginePG) IsRoutineVirtualTable(name string) bool {
	return name == schemaFunctionsVirtualTable
}
