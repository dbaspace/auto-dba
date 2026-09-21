package cdc

import (
	"context"
	"database/sql"
	"strings"
)

type ColumnInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"data_type"`
	IsNullable   *bool  `json:"is_nullable"`
	IsPrimaryKey bool   `json:"is_primary_key"`
	Comment      string `json:"comment"`
}

func SplitSchemaTable(schema, table string) (string, string) {
	table = strings.TrimSpace(table)
	schema = strings.TrimSpace(schema)
	if i := strings.LastIndex(table, "."); i > 0 {
		return table[:i], table[i+1:]
	}
	if schema == "" {
		schema = "public"
	}
	return schema, table
}

func ListPGColumns(ctx context.Context, db *sql.DB, schema, table string) ([]ColumnInfo, error) {
	schema, table = SplitSchemaTable(schema, table)
	rows, err := db.QueryContext(ctx, `
		SELECT c.column_name,
			COALESCE(c.udt_name, c.data_type, ''),
			CASE WHEN c.is_nullable = 'YES' THEN TRUE ELSE FALSE END,
			COALESCE(pgd.description, '')
		FROM information_schema.columns c
		LEFT JOIN pg_catalog.pg_statio_all_tables st
			ON st.schemaname = c.table_schema AND st.relname = c.table_name
		LEFT JOIN pg_catalog.pg_description pgd
			ON pgd.objoid = st.relid AND pgd.objsubid = c.ordinal_position
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		var nullable bool
		if err := rows.Scan(&c.Name, &c.DataType, &nullable, &c.Comment); err != nil {
			return nil, err
		}
		c.IsNullable = &nullable
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pkRows, err := db.QueryContext(ctx, `
		SELECT a.attname FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE i.indisprimary AND n.nspname = $1 AND c.relname = $2`, schema, table)
	if err != nil {
		return cols, nil
	}
	defer pkRows.Close()
	pks := map[string]struct{}{}
	for pkRows.Next() {
		var n string
		if err := pkRows.Scan(&n); err == nil {
			pks[n] = struct{}{}
		}
	}
	for i := range cols {
		if _, ok := pks[cols[i].Name]; ok {
			cols[i].IsPrimaryKey = true
		}
	}
	if cols == nil {
		cols = []ColumnInfo{}
	}
	return cols, nil
}

func IntersectColumns(groups [][]ColumnInfo) []ColumnInfo {
	if len(groups) == 0 {
		return []ColumnInfo{}
	}
	if len(groups) == 1 {
		return groups[0]
	}
	sets := make([]map[string]ColumnInfo, 0, len(groups))
	for _, g := range groups {
		m := make(map[string]ColumnInfo, len(g))
		for _, c := range g {
			m[c.Name] = c
		}
		sets = append(sets, m)
	}
	var out []ColumnInfo
	for _, c := range groups[0] {
		ok := true
		for i := 1; i < len(sets); i++ {
			if _, hit := sets[i][c.Name]; !hit {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	if out == nil {
		out = []ColumnInfo{}
	}
	return out
}
