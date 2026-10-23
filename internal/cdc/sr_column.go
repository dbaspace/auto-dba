package cdc

import (
	"fmt"
	"strconv"
	"strings"
)

func mapPGColToSR(c pgCol) string {
	udt := strings.ToLower(strings.TrimSpace(c.UDT))
	dt := strings.ToLower(strings.TrimSpace(c.DataType))
	switch {
	case udt == "int2" || dt == "smallint":
		return "SMALLINT"
	case udt == "int4" || udt == "serial" || dt == "integer" || dt == "int":
		return "INT"
	case udt == "int8" || udt == "bigserial" || dt == "bigint":
		return "BIGINT"
	case udt == "float4" || dt == "real":
		return "FLOAT"
	case udt == "float8" || dt == "double precision":
		return "DOUBLE"
	case udt == "numeric" || udt == "decimal" || dt == "numeric" || dt == "decimal":
		p := c.NumPrecision
		s := c.NumScale
		if p <= 0 {
			p = 38
		}
		if s < 0 {
			s = 0
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", p, s)
	case udt == "bool" || dt == "boolean":
		return "BOOLEAN"
	case udt == "varchar" || dt == "character varying":
		return srVarcharLen(c.CharMaxLen, true)
	case udt == "char" || udt == "bpchar" || dt == "character":
		return srVarcharLen(c.CharMaxLen, false)
	case udt == "text" || dt == "text":
		return "STRING"
	case pgColIsDateOnly(c):
		return "DATE"
	case pgColIsDateTimeFamily(c):
		return "DATETIME"
	case udt == "time" || dt == "time without time zone" || dt == "time with time zone":
		return "VARCHAR(20)"
	case udt == "json" || udt == "jsonb" || dt == "json" || dt == "jsonb":
		return "JSON"
	case udt == "uuid":
		return "VARCHAR(36)"
	case udt == "bytea":
		return "VARBINARY"
	case udt == "inet" || udt == "cidr":
		return "VARCHAR(50)"
	case strings.HasPrefix(udt, "_"):
		return "JSON"
	default:
		return "VARCHAR(65533)"
	}
}

func srVarcharLen(n int, triple bool) string {
	if n <= 0 {
		return "VARCHAR(65533)"
	}
	if triple {
		n *= 3
	}
	if n > 65533 {
		n = 65533
	}
	return fmt.Sprintf("VARCHAR(%d)", n)
}

func pgColIsDateOnly(c pgCol) bool {
	udt := strings.ToLower(strings.TrimSpace(c.UDT))
	dt := strings.ToLower(strings.TrimSpace(c.DataType))
	return udt == "date" || dt == "date"
}

func pgColHasTimeZone(c pgCol) bool {
	udt := strings.ToLower(strings.TrimSpace(c.UDT))
	dt := strings.ToLower(strings.TrimSpace(c.DataType))
	return udt == "timestamptz" || strings.Contains(dt, "with time zone")
}

func pgColIsTimestampWithoutTZ(c pgCol) bool {
	if pgColHasTimeZone(c) {
		return false
	}
	udt := strings.ToLower(strings.TrimSpace(c.UDT))
	dt := strings.ToLower(strings.TrimSpace(c.DataType))
	return udt == "timestamp" || dt == "timestamp without time zone" ||
		(strings.Contains(dt, "timestamp") && strings.Contains(dt, "without time zone"))
}

func pgColIsDateTimeFamily(c pgCol) bool {
	if pgColIsDateOnly(c) {
		return false
	}
	udt := strings.ToLower(strings.TrimSpace(c.UDT))
	dt := strings.ToLower(strings.TrimSpace(c.DataType))
	return udt == "timestamp" || udt == "timestamptz" || strings.Contains(dt, "timestamp")
}

func orderPgColsByKeys(cols []pgCol, keys []string) []pgCol {
	if len(cols) == 0 {
		return nil
	}
	byName := make(map[string]pgCol, len(cols))
	for _, c := range cols {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		byName[strings.ToLower(c.Name)] = c
	}
	seen := map[string]struct{}{}
	out := make([]pgCol, 0, len(cols))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if c, ok := byName[strings.ToLower(k)]; ok {
			out = append(out, c)
			seen[strings.ToLower(k)] = struct{}{}
		}
	}
	for _, c := range cols {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		if _, ok := seen[strings.ToLower(c.Name)]; ok {
			continue
		}
		out = append(out, c)
	}
	return out
}

func sharedPgCols(src []string, colsByTable map[string][]pgCol) []pgCol {
	var groups [][]pgCol
	for _, name := range src {
		cols := colsByTable[name]
		if len(cols) == 0 {
			continue
		}
		groups = append(groups, cols)
	}
	if len(groups) == 0 {
		return nil
	}
	if len(groups) == 1 {
		return append([]pgCol(nil), groups[0]...)
	}
	sets := make([]map[string]struct{}, 0, len(groups)-1)
	for _, g := range groups[1:] {
		m := make(map[string]struct{}, len(g))
		for _, c := range g {
			m[strings.ToLower(strings.TrimSpace(c.Name))] = struct{}{}
		}
		sets = append(sets, m)
	}
	var out []pgCol
	for _, c := range groups[0] {
		key := strings.ToLower(strings.TrimSpace(c.Name))
		if key == "" {
			continue
		}
		ok := true
		for _, set := range sets {
			if _, hit := set[key]; !hit {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

func escapeSRComment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, "'", "''")
}

func escapeSRStringDefault(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func srQuotedDefaultLiteral(s string) string {
	return `"` + escapeSRStringDefault(s) + `"`
}

func srTypeAllowsColumnDefault(srType string) bool {
	upper := strings.ToUpper(strings.TrimSpace(srType))
	switch upper {
	case "JSON", "VARBINARY", "BITMAP", "HLL":
		return false
	}
	if strings.HasPrefix(upper, "ARRAY") {
		return false
	}
	return true
}

func pgDefaultLooksLikeIdentity(expr string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(expr)), "nextval(")
}

func parsePGStringLiteralDefault(expr string) string {
	expr = strings.TrimSpace(expr)
	if i := strings.Index(expr, "::"); i > 0 {
		expr = strings.TrimSpace(expr[:i])
	}
	if len(expr) < 2 || expr[0] != '\'' || expr[len(expr)-1] != '\'' {
		return ""
	}
	inner := expr[1 : len(expr)-1]
	return strings.ReplaceAll(inner, "''", "'")
}

func mapPGDefaultToSR(c pgCol, srType string) string {
	if c.IsIdentity || pgDefaultLooksLikeIdentity(c.DefaultExpr) {
		return ""
	}
	if !srTypeAllowsColumnDefault(srType) {
		return ""
	}
	if pgColIsDateOnly(c) {
		if !c.IsNullable {
			return srQuotedDefaultLiteral("1970-01-01")
		}
		return ""
	}
	if pgColIsDateTimeFamily(c) {
		if !c.IsNullable {
			return "current_timestamp"
		}
		return ""
	}
	expr := strings.TrimSpace(c.DefaultExpr)
	if expr == "" {
		return ""
	}
	lower := strings.ToLower(expr)
	switch lower {
	case "current_date":
		if strings.EqualFold(strings.TrimSpace(srType), "DATE") && !c.IsNullable {
			return srQuotedDefaultLiteral("1970-01-01")
		}
		return ""
	case "current_timestamp", "now()":
		if strings.EqualFold(strings.TrimSpace(srType), "DATETIME") {
			return "current_timestamp"
		}
		return ""
	case "true", "false":
		return srQuotedDefaultLiteral(lower)
	}
	if _, err := strconv.ParseFloat(expr, 64); err == nil {
		return srQuotedDefaultLiteral(expr)
	}
	if lit := parsePGStringLiteralDefault(expr); lit != "" || strings.HasPrefix(expr, "'") {
		if strings.HasPrefix(lower, "'{}'::") {
			return srQuotedDefaultLiteral("{}")
		}
		return srQuotedDefaultLiteral(lit)
	}
	if strings.HasPrefix(lower, "'{}'::") {
		return srQuotedDefaultLiteral("{}")
	}
	return ""
}

func formatSRColumnLine(c pgCol, typ, nullDecl string) string {
	parts := []string{fmt.Sprintf("`%s`", c.Name), typ, nullDecl}
	if def := mapPGDefaultToSR(c, typ); def != "" {
		parts = append(parts, "DEFAULT "+def)
	}
	if cmt := escapeSRComment(c.Comment); cmt != "" {
		parts = append(parts, "COMMENT '"+cmt+"'")
	}
	return "  " + strings.Join(parts, " ")
}
