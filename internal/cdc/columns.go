package cdc

import (
	"context"
	"database/sql"
	"fmt"
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
	got, err := ListPGColumnsMany(ctx, db, schema, []string{table})
	if err != nil {
		return nil, err
	}
	cols := got[strings.TrimSpace(table)]
	if cols == nil {
		_, bare := SplitSchemaTable(schema, table)
		cols = got[bare]
	}
	if cols == nil {
		cols = []ColumnInfo{}
	}
	return cols, nil
}

// ListPGColumnsMany 按 schema 一次查出多张表的列和主键（pg_catalog，各一条 SQL）。
// 返回的 key 与入参表名（trim 后）一致。
func ListPGColumnsMany(ctx context.Context, db *sql.DB, schema string, tables []string) (map[string][]ColumnInfo, error) {
	if db == nil {
		return nil, fmt.Errorf("db 必填")
	}
	out := map[string][]ColumnInfo{}
	grouped := map[string][]string{}
	order := []string{}
	for _, raw := range tables {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, ok := out[raw]; ok {
			continue
		}
		out[raw] = []ColumnInfo{}
		sch, tb := SplitSchemaTable(schema, raw)
		if _, ok := grouped[sch]; !ok {
			order = append(order, sch)
		}
		grouped[sch] = append(grouped[sch], tb)
	}
	for _, sch := range order {
		part, err := listPGColumnsSchema(ctx, db, sch, grouped[sch])
		if err != nil {
			return nil, err
		}
		for key := range out {
			ks, tb := SplitSchemaTable(schema, key)
			if ks != sch {
				continue
			}
			if cols, ok := part[tb]; ok {
				out[key] = cols
			}
		}
	}
	return out, nil
}

func listPGColumnsSchema(ctx context.Context, db *sql.DB, schema string, tables []string) (map[string][]ColumnInfo, error) {
	out := map[string][]ColumnInfo{}
	const chunk = 80
	for start := 0; start < len(tables); start += chunk {
		end := start + chunk
		if end > len(tables) {
			end = len(tables)
		}
		part := tables[start:end]
		ph := make([]string, len(part))
		args := make([]any, 0, len(part)+1)
		args = append(args, schema)
		for i, name := range part {
			ph[i] = fmt.Sprintf("$%d", i+2)
			args = append(args, name)
			out[name] = []ColumnInfo{}
		}
		in := strings.Join(ph, ",")
		rows, err := db.QueryContext(ctx, `
			SELECT c.relname, a.attname, COALESCE(t.typname, ''), NOT a.attnotnull, COALESCE(d.description, '')
			FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
			JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
			LEFT JOIN pg_catalog.pg_description d ON d.objoid = c.oid AND d.objsubid = a.attnum
			WHERE n.nspname = $1 AND c.relname IN (`+in+`)
			ORDER BY c.relname, a.attnum`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var rel string
			var c ColumnInfo
			var nullable bool
			if err := rows.Scan(&rel, &c.Name, &c.DataType, &nullable, &c.Comment); err != nil {
				rows.Close()
				return nil, err
			}
			c.IsNullable = &nullable
			out[rel] = append(out[rel], c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()

		pkRows, err := db.QueryContext(ctx, `
			SELECT c.relname, a.attname
			FROM pg_catalog.pg_index i
			JOIN pg_catalog.pg_class c ON c.oid = i.indrelid
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY(i.indkey)
			WHERE i.indisprimary AND n.nspname = $1 AND c.relname IN (`+in+`)`, args...)
		if err != nil {
			continue
		}
		pks := map[string]map[string]struct{}{}
		for pkRows.Next() {
			var rel, name string
			if err := pkRows.Scan(&rel, &name); err != nil {
				continue
			}
			if pks[rel] == nil {
				pks[rel] = map[string]struct{}{}
			}
			pks[rel][name] = struct{}{}
		}
		pkRows.Close()
		for rel, cols := range out {
			set := pks[rel]
			if len(set) == 0 {
				continue
			}
			for i := range cols {
				if _, ok := set[cols[i].Name]; ok {
					cols[i].IsPrimaryKey = true
				}
			}
			out[rel] = cols
		}
	}
	return out, nil
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
