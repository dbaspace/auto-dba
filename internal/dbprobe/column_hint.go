package dbprobe

import (
	"context"
	"strings"

	"auto-dba/internal/store"
)

// HintColumn 查询窗口补全用的字段，不含默认值和建表语句。
type HintColumn struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Nullable bool   `json:"nullable"`
	Comment  string `json:"comment,omitempty"`
}

// HintColumnsSupported 只有 MySQL 族和 PostgreSQL 查字段。其它类型调用方应在连库前返回空。
func HintColumnsSupported(dbType string) bool {
	switch family(dbType) {
	case "mysql", "postgres":
		return true
	default:
		return false
	}
}

// SplitHintTable 把 schema.table 拆开。没有点时 schema 为空，PostgreSQL 再按 public 查。
func SplitHintTable(ref string) (schema, table string) {
	ref = strings.TrimSpace(ref)
	ref = strings.Trim(ref, "`\"")
	if i := strings.LastIndex(ref, "."); i >= 0 {
		schema = strings.Trim(ref[:i], "`\" ")
		table = strings.Trim(ref[i+1:], "`\" ")
		return schema, table
	}
	return "", strings.Trim(ref, "`\" ")
}

// ListHintColumns 对一张表只发一条字典查询，不取建表语句，也不扫业务表。
func ListHintColumns(ctx context.Context, inst *store.Instance, password, dbName, schema, table string) (string, []HintColumn, error) {
	table = strings.TrimSpace(table)
	if table == "" {
		return schema, nil, errHintTable
	}
	host, user, dbType, err := instConn(inst)
	if err != nil {
		return schema, nil, err
	}
	switch family(dbType) {
	case "mysql":
		cols, err := listMySQLHintColumns(ctx, host, inst.Port, user, password, dbName, table)
		return dbName, cols, err
	case "postgres":
		if strings.TrimSpace(schema) == "" {
			schema = "public"
		}
		cols, err := listPostgresHintColumns(ctx, host, inst.Port, user, password, dbName, schema, table)
		return schema, cols, err
	default:
		return schema, []HintColumn{}, nil
	}
}
