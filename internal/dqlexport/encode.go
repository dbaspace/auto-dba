package dqlexport

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"auto-dba/internal/dbprobe"
)

var reFromTable = regexp.MustCompile(`(?i)\bfrom\s+(.+?)(?:\s+(?:as|where|limit|order|group|having|join|left|right|inner|outer|on|union)|;|$)`)

// Encode 将查询结果编码为 csv / json / sql。
func Encode(res *dbprobe.QueryResult, format, table, dbType string) (body []byte, ext, contentType string) {
	if res == nil {
		res = &dbprobe.QueryResult{}
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return encodeJSON(res), "json", "application/json; charset=utf-8"
	case "sql":
		return encodeSQL(res, table, dbType), "sql", "application/sql; charset=utf-8"
	default:
		return encodeCSV(res), "csv", "text/csv; charset=utf-8"
	}
}

func encodeCSV(res *dbprobe.QueryResult) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	_ = w.Write(res.Columns)
	for _, row := range res.Rows {
		rec := make([]string, len(res.Columns))
		for i := range res.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			rec[i] = CellString(v)
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return buf.Bytes()
}

func encodeJSON(res *dbprobe.QueryResult) []byte {
	out := make([]map[string]any, 0, len(res.Rows))
	for _, row := range res.Rows {
		m := make(map[string]any, len(res.Columns))
		for i, col := range res.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			m[col] = v
		}
		out = append(out, m)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return b
}

func encodeSQL(res *dbprobe.QueryResult, table, dbType string) []byte {
	if table == "" {
		table = "export_result"
	}
	var b strings.Builder
	b.WriteString("-- auto-dba dql export\n")
	if len(res.Columns) == 0 {
		return []byte(b.String())
	}
	cols := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		cols[i] = QuoteIdent(c, dbType)
	}
	ident := QuoteIdent(table, dbType)
	if len(res.Rows) == 0 {
		b.WriteString(fmt.Sprintf("-- no rows\n-- INSERT INTO %s (%s) VALUES ();\n", ident, strings.Join(cols, ", ")))
		return []byte(b.String())
	}
	for _, row := range res.Rows {
		vals := make([]string, len(res.Columns))
		for i := range res.Columns {
			var v any
			if i < len(row) {
				v = row[i]
			}
			vals[i] = SQLLiteral(v)
		}
		b.WriteString("INSERT INTO ")
		b.WriteString(ident)
		b.WriteString(" (")
		b.WriteString(strings.Join(cols, ", "))
		b.WriteString(") VALUES (")
		b.WriteString(strings.Join(vals, ", "))
		b.WriteString(");\n")
	}
	return []byte(b.String())
}

// TableNameFromSQL 从 SELECT … FROM 粗提表名，供 SQL 导出 INSERT 使用。
func TableNameFromSQL(sqlText string) string {
	m := reFromTable.FindStringSubmatch(sqlText)
	if len(m) < 2 {
		return "export_result"
	}
	parts := strings.Split(m[1], ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), "`\"'[]")
	}
	name := strings.Join(parts, ".")
	if name == "" || strings.ContainsAny(name, " \t\n()=") {
		return "export_result"
	}
	return name
}

func QuoteIdent(name, dbType string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "`", "")
	if dbType == "postgres" || dbType == "postgresql" {
		return `"` + strings.ReplaceAll(name, `.`, `"."`) + `"`
	}
	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		for i, p := range parts {
			parts[i] = "`" + p + "`"
		}
		return strings.Join(parts, ".")
	}
	return "`" + name + "`"
}

func SQLLiteral(v any) string {
	if v == nil {
		return "NULL"
	}
	switch t := v.(type) {
	case bool:
		if t {
			return "TRUE"
		}
		return "FALSE"
	case int, int32, int64, float32, float64, json.Number:
		return fmt.Sprint(t)
	default:
		s := CellString(v)
		s = strings.ReplaceAll(s, `'`, `''`)
		return "'" + s + "'"
	}
}

func CellString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprint(t)
	}
}

// SanitizeFilePart 文件名安全片段。
func SanitizeFilePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "export"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
