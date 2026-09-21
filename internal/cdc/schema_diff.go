package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type SchemaDiffColumn struct {
	Name     string `json:"name"`
	PGType   string `json:"pg_type"`
	SRType   string `json:"sr_type"`
	Selected bool   `json:"selected"`
	Nullable bool   `json:"is_nullable,omitempty"`
	Primary  bool   `json:"is_primary_key,omitempty"`
}

type SchemaDiffItem struct {
	TargetTable  string             `json:"target_table"`
	SourceTables []string           `json:"source_tables"`
	ColumnsToAdd []SchemaDiffColumn `json:"columns_to_add"`
	Warnings     []string           `json:"warnings,omitempty"`
	Error        string             `json:"error,omitempty"`
}

func MapPGTypeToSR(dataType string) string {
	dt := strings.ToLower(strings.TrimSpace(dataType))
	switch {
	case strings.Contains(dt, "int8"), strings.Contains(dt, "bigint"):
		return "BIGINT"
	case strings.Contains(dt, "int4"), dt == "int", strings.Contains(dt, "integer"):
		return "INT"
	case strings.Contains(dt, "int2"), strings.Contains(dt, "smallint"):
		return "SMALLINT"
	case strings.Contains(dt, "bool"):
		return "BOOLEAN"
	case strings.Contains(dt, "float8"), strings.Contains(dt, "double"):
		return "DOUBLE"
	case strings.Contains(dt, "float4"), dt == "real":
		return "FLOAT"
	case strings.Contains(dt, "numeric"), strings.Contains(dt, "decimal"):
		return "DECIMAL(38,10)"
	case strings.Contains(dt, "timestamp"):
		return "DATETIME"
	case dt == "date":
		return "DATE"
	case strings.Contains(dt, "json"):
		return "JSON"
	default:
		return "STRING"
	}
}

func ListSRColumns(ctx context.Context, db *sql.DB, schema, table string) ([]string, error) {
	schema, table = SplitSchemaTable(schema, table)
	if schema == "" {
		schema = "public"
	}
	rows, err := db.QueryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil && n != "" && !IsSRMetaColumn(n) {
			names = append(names, n)
		}
	}
	if names == nil {
		names = []string{}
	}
	return names, rows.Err()
}

func DiffPGMinusSR(pgCols []ColumnInfo, srNames []string) []SchemaDiffColumn {
	sr := map[string]struct{}{}
	for _, n := range srNames {
		sr[strings.ToLower(strings.TrimSpace(n))] = struct{}{}
	}
	var out []SchemaDiffColumn
	for _, c := range pgCols {
		name := strings.TrimSpace(c.Name)
		if name == "" || c.IsPrimaryKey {
			continue
		}
		if _, ok := sr[strings.ToLower(name)]; ok {
			continue
		}
		nullable := false
		if c.IsNullable != nil {
			nullable = *c.IsNullable
		}
		out = append(out, SchemaDiffColumn{
			Name:     name,
			PGType:   c.DataType,
			SRType:   MapPGTypeToSR(c.DataType),
			Selected: true,
			Nullable: nullable,
		})
	}
	if out == nil {
		out = []SchemaDiffColumn{}
	}
	return out
}

func BuildAlterAddColumnSQL(targetDB, targetTable string, cols []SchemaDiffColumn) string {
	if strings.TrimSpace(targetDB) == "" || strings.TrimSpace(targetTable) == "" || len(cols) == 0 {
		return ""
	}
	var adds []string
	for _, c := range cols {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		typ := strings.TrimSpace(c.SRType)
		if typ == "" {
			typ = MapPGTypeToSR(c.PGType)
		}
		adds = append(adds, fmt.Sprintf("ADD COLUMN `%s` %s", c.Name, typ))
	}
	if len(adds) == 0 {
		return ""
	}
	return fmt.Sprintf("ALTER TABLE `%s`.`%s` %s;", targetDB, targetTable, strings.Join(adds, ", "))
}

func IsSRMetaColumn(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "__op" || n == "__deleted" || n == "__db" || n == "__table" || n == "__ts_ms"
}
