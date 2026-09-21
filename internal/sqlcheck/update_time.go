package sqlcheck

import (
	"regexp"
	"strings"
)

var (
	reCreateIndexOn = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?[^\s(]+\s+ON\s+([^\s(]+)(?:\s+USING\s+[^\s(]+)?\s*\((.*)\)`)
	reCreateTrigger = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\s+\S+\s+(.+?)\s+ON\s+(\S+)\s+(.*)$`)
	reIndexKeyDir   = regexp.MustCompile(`(?i)\s+(ASC|DESC|NULLS\s+(FIRST|LAST))\b`)
	reColUnique     = regexp.MustCompile(`(?i)\bUNIQUE\b`)
)

func applyRequireUpdateTimeRules(stmts []string, byName map[string]Rule, res *Result) {
	if res == nil || !enabled(byName, "DDLRequireUpdateTime") {
		return
	}
	r, _ := ruleOf(byName, "DDLRequireUpdateTime")
	colName := strings.ToLower(strings.TrimSpace(ruleField(r.Value, "column", "update_time")))
	if colName == "" {
		colName = "update_time"
	}
	allowed := ruleStringSlice(r.Value, "allowed_types", []string{
		"timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone",
	})

	triggerOK := map[string]bool{}
	indexedCols := map[string]map[string]bool{}
	for _, stmt := range stmts {
		norm := strings.Join(strings.Fields(strings.TrimSpace(stripSQLComments(stmt))), " ")
		if tbl, cols, ok := parseCreateIndexCols(norm); ok {
			key := normalizeTableRef(tbl)
			if key == "" {
				continue
			}
			if indexedCols[key] == nil {
				indexedCols[key] = map[string]bool{}
			}
			for _, c := range cols {
				if ident := indexColumnIdent(c); ident != "" {
					indexedCols[key][ident] = true
				}
			}
			continue
		}
		if tbl, ok := parseBeforeUpdateRowTrigger(norm); ok {
			if key := normalizeTableRef(tbl); key != "" {
				triggerOK[key] = true
			}
		}
	}

	for i, stmt := range stmts {
		upper := strings.ToUpper(stripSQLComments(stmt))
		if !reCreateTable.MatchString(upper) || reCreateTableAS.MatchString(upper) {
			continue
		}
		table := createTableRef(stmt)
		tblKey := normalizeTableRef(table)
		display := strings.TrimSpace(table)
		if display == "" {
			display = tblKey
		}
		var missing []string

		colDef, hasCol := findCreateTableColumnDef(stmt, colName)
		if !hasCol {
			missing = append(missing, "缺少列 "+colName+"（时间类型）")
		} else if !pgTimeTypeAllowed(columnDataType(colDef), allowed) {
			missing = append(missing, "列 "+colName+" 类型须为时间类型，当前为 "+columnDataType(colDef))
		}
		if !triggerOK[tblKey] {
			missing = append(missing, "缺少 BEFORE UPDATE FOR EACH ROW 触发器（需在工单中包含 CREATE TRIGGER ... BEFORE UPDATE ON "+display+" FOR EACH ROW ...）")
		}
		if !createTableHasColumnIndex(stmt, colName, indexedCols[tblKey]) {
			missing = append(missing, "列 "+colName+" 缺少索引（需 CREATE INDEX ... ON "+display+" ("+colName+") 或 UNIQUE）")
		}
		if len(missing) == 0 {
			continue
		}
		markStmt(res, i, errID("DDLRequireUpdateTime", "建表须满足 "+colName+" 规范："+strings.Join(missing, "；")))
	}
}

func parseCreateIndexCols(norm string) (table string, cols []string, ok bool) {
	m := reCreateIndexOn.FindStringSubmatch(norm)
	if len(m) < 3 {
		return "", nil, false
	}
	table = strings.TrimSpace(m[1])
	raw := strings.TrimSpace(m[2])
	if table == "" || raw == "" {
		return "", nil, false
	}
	return table, splitIndexCols(raw), true
}

func parseBeforeUpdateRowTrigger(norm string) (table string, ok bool) {
	m := reCreateTrigger.FindStringSubmatch(norm)
	if len(m) < 4 {
		return "", false
	}
	timing := strings.ToUpper(strings.TrimSpace(m[1]))
	table = strings.TrimSpace(m[2])
	rest := strings.ToUpper(strings.TrimSpace(m[3]))
	if !strings.Contains(timing, "BEFORE") || !strings.Contains(timing, "UPDATE") {
		return "", false
	}
	if !strings.Contains(rest, "FOR EACH ROW") {
		return "", false
	}
	if table == "" {
		return "", false
	}
	return table, true
}

func findCreateTableColumnDef(stmt, colName string) (def string, ok bool) {
	want := strings.ToLower(colName)
	for _, d := range createTableColumnDefs(stmt) {
		if isConstraintDef(d) {
			continue
		}
		if strings.ToLower(firstIdent(d)) == want {
			return d, true
		}
	}
	return "", false
}

func columnDataType(def string) string {
	def = strings.TrimSpace(def)
	m := reFirstIdent.FindStringSubmatch(def)
	if len(m) < 2 {
		return ""
	}
	rest := strings.TrimSpace(def[len(m[0]):])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	dt := strings.ToUpper(fields[0])
	// timestamp with/without time zone
	joined := strings.ToUpper(strings.Join(fields, " "))
	switch {
	case strings.HasPrefix(joined, "TIMESTAMP WITH TIME ZONE"):
		return "TIMESTAMP WITH TIME ZONE"
	case strings.HasPrefix(joined, "TIMESTAMP WITHOUT TIME ZONE"):
		return "TIMESTAMP"
	case strings.HasPrefix(dt, "TIMESTAMPTZ"):
		return "TIMESTAMPTZ"
	case strings.HasPrefix(dt, "TIMESTAMP"):
		return "TIMESTAMP"
	default:
		// varchar(128) -> VARCHAR
		if i := strings.IndexByte(dt, '('); i > 0 {
			dt = dt[:i]
		}
		return dt
	}
}

func createTableHasColumnIndex(stmt, colName string, idxCols map[string]bool) bool {
	want := strings.ToLower(colName)
	if idxCols[want] {
		return true
	}
	if def, ok := findCreateTableColumnDef(stmt, colName); ok && reColUnique.MatchString(def) {
		return true
	}
	for _, d := range createTableColumnDefs(stmt) {
		up := strings.ToUpper(strings.TrimSpace(d))
		if !(strings.HasPrefix(up, "CONSTRAINT") && strings.Contains(up, "UNIQUE")) && !strings.HasPrefix(up, "UNIQUE") {
			continue
		}
		body := ""
		if i := strings.Index(d, "("); i >= 0 {
			if j := strings.LastIndex(d, ")"); j > i {
				body = d[i+1 : j]
			}
		}
		for _, c := range splitIndexCols(body) {
			if strings.EqualFold(strings.Trim(c, `"'`), colName) {
				return true
			}
		}
	}
	return false
}

func indexColumnIdent(raw string) string {
	c := strings.TrimSpace(raw)
	c = reIndexKeyDir.ReplaceAllString(c, "")
	c = strings.TrimSpace(c)
	c = strings.Trim(c, `"'`)
	if f := strings.Fields(c); len(f) > 0 {
		c = f[0]
	}
	return normalizeIdent(c)
}

func pgTimeTypeAllowed(dataType string, allowed []string) bool {
	dt := strings.ToUpper(strings.TrimSpace(dataType))
	if dt == "" {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		na := strings.Join(strings.Fields(strings.ToLower(a)), " ")
		switch na {
		case "timestamp", "timestamp without time zone":
			if dt == "TIMESTAMP" {
				return true
			}
		case "timestamptz", "timestamp with time zone":
			if dt == "TIMESTAMPTZ" || dt == "TIMESTAMP WITH TIME ZONE" {
				return true
			}
		default:
			if dt == strings.ToUpper(na) {
				return true
			}
		}
	}
	return false
}
