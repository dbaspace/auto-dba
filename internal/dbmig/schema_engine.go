package dbmig

import (
	"context"
	"database/sql"
	"fmt"

	"auto-dba/internal/store"
)

const (
	schemaFunctionsVirtualTable = "__schema_functions__"
	schemaRoutinesVirtualTable  = "__schema_routines__"
)

// schemaEngine 表结构对比/修复的引擎适配器。
type schemaEngine interface {
	FetchAndDiff(ctx context.Context, left, right *sql.DB, leftSchema, rightSchema, table string) (TableSchemaDiff, error)
	DiffRoutines(ctx context.Context, left, right *sql.DB, leftSchema, rightSchema string) []TableSchemaDiff
	BuildCreateSQL(ctx context.Context, src *sql.DB, leftSchema, rightSchema, table string) (string, error)
	BuildAlterSQL(ctx context.Context, src *sql.DB, leftSchema, rightSchema string, item *TableSchemaDiff) (string, error)
	BuildRoutineFixSQL(ctx context.Context, src *sql.DB, leftSchema, rightSchema string, item *TableSchemaDiff) (string, error)
	IsRoutineVirtualTable(name string) bool
}

func selectSchemaEngine(engine string) (schemaEngine, error) {
	switch store.NormalizeDBType(engine) {
	case "postgres":
		return schemaEnginePG{}, nil
	case "mysql", "tidb", "oceanbase":
		return schemaEngineMySQL{}, nil
	default:
		return nil, fmt.Errorf("仅支持 MySQL / PostgreSQL 结构对比")
	}
}
