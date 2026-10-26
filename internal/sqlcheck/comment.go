package sqlcheck

import (
	"regexp"
	"strings"
)

type tableCol struct {
	table string
	col   string
}

func applyTicketCommentRules(stmts []string, dbType string, byName map[string]Rule, res *Result) {
	if res == nil || len(stmts) == 0 {
		return
	}
	needCol := enabled(byName, "check_column_comment", "DDLCheckColumnComment")
	needTbl := enabled(byName, "check_table_comment", "DDLCheckTableComment")
	if !needCol && !needTbl {
		return
	}
	if dbType == "postgres" {
		applyPostgresCommentRules(stmts, needCol, needTbl, res)
		return
	}
	applyInlineCommentRules(stmts, dbType, needCol, needTbl, byName, res)
}

func applyPostgresCommentRules(stmts []string, needCol, needTbl bool, res *Result) {
	commentedCols := map[tableCol]bool{}
	commentedTables := map[string]bool{}
	commentedViews := map[string]bool{}
	for _, stmt := range stmts {
		if t, c, text, ok := parseCommentOnColumn(stmt); ok && strings.TrimSpace(text) != "" {
			commentedCols[tableCol{t, c}] = true
		}
		if t, text, ok := parseCommentOnTable(stmt); ok && strings.TrimSpace(text) != "" {
			commentedTables[t] = true
		}
		if t, kind, text, ok := parseCommentOnView(stmt); ok && strings.TrimSpace(text) != "" {
			if kind == "view" || kind == "materialized view" {
				commentedViews[t] = true
			}
		}
	}
	for i, stmt := range stmts {
		upper := strings.ToUpper(stmt)
		if isAlterAddColumn(upper) && needCol {
			table := alterTableName(stmt)
			for _, col := range alterAddedColumns(stmt) {
				if !commentedCols[tableCol{normalizeTableRef(table), normalizeIdent(col)}] {
					markStmt(res, i, errID("DDLCheckColumnComment", postgresMissingColumnComment(table, col)))
					break
				}
			}
		}
		if !reCreateTable.MatchString(upper) {
			if needTbl && reCreateView.MatchString(upper) {
				name, materialized := createViewName(stmt)
				if name != "" && !commentedViews[normalizeTableRef(name)] && !commentedTables[normalizeTableRef(name)] {
					kw := "COMMENT ON VIEW"
					if materialized {
						kw = "COMMENT ON MATERIALIZED VIEW"
					}
					markStmt(res, i, errID("DDLCheckTableComment", "CREATE VIEW 缺少注释（需在工单中包含 "+kw+" "+name+" IS '...'）"))
				}
			}
			continue
		}
		table := createTableRef(stmt)
		isCTAS := reCreateTableAS.MatchString(upper)
		var missing []string
		needTableComment := needTbl || (needCol && !isCTAS)
		if needTableComment && table != "" && !commentedTables[normalizeTableRef(table)] {
			missing = append(missing, "缺少表 "+displayTable(table)+" 的表注释（需在工单中包含 COMMENT ON TABLE "+displayTable(table)+" IS '...'）")
		}
		if needCol && !isCTAS {
			for _, col := range createTableColumns(stmt) {
				if !commentedCols[tableCol{normalizeTableRef(table), normalizeIdent(col)}] {
					missing = append(missing, "缺少列 "+displayTable(table)+"."+col+" 的列注释（需在工单中包含 COMMENT ON COLUMN "+displayTable(table)+"."+col+" IS '...'）")
				}
			}
		}
		if len(missing) > 0 {
			id := "DDLCheckCreateTableComment"
			if isCTAS || !needCol {
				id = "DDLCheckTableComment"
			}
			markStmt(res, i, errID(id, "缺少约束："+strings.Join(missing, "；")))
		}
	}
}

func applyInlineCommentRules(stmts []string, dbType string, needCol, needTbl bool, byName map[string]Rule, res *Result) {
	for i, stmt := range stmts {
		upper := strings.ToUpper(stmt)
		if isAlterAddColumn(upper) && needCol {
			for _, def := range alterAddedColumnDefs(stmt) {
				if !hasNonEmptyInlineComment(def) {
					col := firstIdent(def)
					markStmt(res, i, errID("DDLCheckColumnComment", inlineMissingColumnComment(col)))
					break
				}
			}
		}
		if !reCreateTable.MatchString(upper) {
			continue
		}
		isCTAS := reCreateTableAS.MatchString(upper)
		if needTbl && !hasNonEmptyTableComment(stmt) {
			markStmt(res, i, errID("DDLCheckTableComment",
				ruleMsg(byName, "DDLCheckTableComment", "强制表必须拥有表注释（须包含 COMMENT）")))
		}
		if needCol && !isCTAS {
			for _, def := range createTableColumnDefs(stmt) {
				if isConstraintDef(def) {
					continue
				}
				if !hasNonEmptyInlineComment(def) {
					markStmt(res, i, errID("DDLCheckColumnComment", "强制表字段必须拥有列注释（须包含 COMMENT '...'）"))
					break
				}
			}
		}
		_ = dbType
	}
}

func postgresMissingColumnComment(table, col string) string {
	table = displayTable(table)
	return "新增列 " + table + "." + col + " 缺少列注释（需在工单中包含 COMMENT ON COLUMN " + table + "." + col + " IS '...'）"
}

func inlineMissingColumnComment(col string) string {
	if strings.TrimSpace(col) == "" {
		col = "该列"
	}
	return "新增列 " + col + " 必须拥有列注释（须包含 COMMENT '...'）"
}

func markStmt(res *Result, idx int, hit *ruleHit) {
	if res == nil || hit == nil || idx < 0 || idx >= len(res.Statements) {
		return
	}
	row := &res.Statements[idx]
	if row.ErrorLevel != "ERROR" {
		row.ErrorLevel, row.Message, row.RuleID = hit.Level, hit.Message, hit.RuleID
	}
	if hit.Level == "ERROR" {
		res.OK = false
		res.Errors = append(res.Errors, map[string]any{"message": hit.Message, "sql": row.SQL, "rule_id": hit.RuleID})
		return
	}
	if hit.Level == "WARN" {
		res.Warns = append(res.Warns, map[string]any{"message": hit.Message, "sql": row.SQL, "rule_id": hit.RuleID})
	}
}

func parseCommentOnColumn(stmt string) (table, col, text string, ok bool) {
	m := reCommentOnColumn.FindStringSubmatch(stmt)
	if len(m) < 4 {
		return "", "", "", false
	}
	parts := splitQualified(m[1])
	if len(parts) < 2 {
		return "", "", "", false
	}
	col = normalizeIdent(parts[len(parts)-1])
	table = normalizeIdent(parts[len(parts)-2])
	text = firstNonEmpty(m[2], m[3])
	text = strings.ReplaceAll(text, "''", "'")
	return table, col, text, table != "" && col != ""
}

func parseCommentOnTable(stmt string) (table, text string, ok bool) {
	m := reCommentOnTable.FindStringSubmatch(stmt)
	if len(m) < 4 {
		return "", "", false
	}
	table = normalizeTableRef(m[1])
	text = firstNonEmpty(m[2], m[3])
	return table, text, table != ""
}

func parseCommentOnView(stmt string) (name, kind, text string, ok bool) {
	m := reCommentOnView.FindStringSubmatch(stmt)
	if len(m) < 5 {
		return "", "", "", false
	}
	kind = strings.ToLower(strings.Join(strings.Fields(m[1]), " "))
	name = normalizeTableRef(m[2])
	text = firstNonEmpty(m[3], m[4])
	return name, kind, text, name != ""
}

func alterTableName(stmt string) string {
	m := reAlterTableName.FindStringSubmatch(stmt)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func alterAddedColumns(stmt string) []string {
	var cols []string
	for _, def := range alterAddedColumnDefs(stmt) {
		if name := firstIdent(def); name != "" {
			cols = append(cols, name)
		}
	}
	return cols
}

func alterAddedColumnDefs(stmt string) []string {
	up := strings.ToUpper(stmt)
	if !reAlterAdd.MatchString(up) {
		return nil
	}
	locs := reAddOp.FindAllStringIndex(stmt, -1)
	var defs []string
	for i, loc := range locs {
		start := loc[1]
		end := len(stmt)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		def := strings.TrimSpace(stmt[start:end])
		def = strings.Trim(def, ";,")
		if def == "" || isNonColumnAdd(def) {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(def), "COLUMN ") {
			def = strings.TrimSpace(def[len("COLUMN"):])
		}
		// PostgreSQL: ADD [COLUMN] [IF NOT EXISTS] column_name
		if strings.HasPrefix(strings.ToUpper(def), "IF NOT EXISTS ") {
			def = strings.TrimSpace(def[len("IF NOT EXISTS"):])
		}
		if def != "" {
			defs = append(defs, def)
		}
	}
	return defs
}

func isNonColumnAdd(def string) bool {
	up := strings.ToUpper(strings.TrimSpace(def))
	for _, p := range []string{"CONSTRAINT ", "INDEX ", "UNIQUE ", "UNIQUE(", "PRIMARY ", "FULLTEXT", "SPATIAL", "KEY ", "CHECK ", "CHECK(", "FOREIGN "} {
		if strings.HasPrefix(up, p) {
			return true
		}
	}
	return false
}

func createTableColumns(stmt string) []string {
	var cols []string
	for _, def := range createTableColumnDefs(stmt) {
		if isConstraintDef(def) {
			continue
		}
		if name := firstIdent(def); name != "" {
			cols = append(cols, name)
		}
	}
	return cols
}

func createTableColumnDefs(stmt string) []string {
	body := tableDefBody(stmt)
	if body == "" {
		return nil
	}
	return splitTopLevel(body)
}

func tableDefBody(stmt string) string {
	i := strings.Index(stmt, "(")
	if i < 0 {
		return ""
	}
	depth := 0
	for j := i; j < len(stmt); j++ {
		switch stmt[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return stmt[i+1 : j]
			}
		}
	}
	return ""
}

func splitTopLevel(s string) []string {
	var out []string
	depth, last := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(s[last:i])
				if part != "" {
					out = append(out, part)
				}
				last = i + 1
			}
		}
	}
	if part := strings.TrimSpace(s[last:]); part != "" {
		out = append(out, part)
	}
	return out
}

func isConstraintDef(def string) bool {
	up := strings.ToUpper(strings.TrimSpace(def))
	return strings.HasPrefix(up, "CONSTRAINT") || strings.HasPrefix(up, "PRIMARY ") ||
		strings.HasPrefix(up, "UNIQUE") || strings.HasPrefix(up, "CHECK") ||
		strings.HasPrefix(up, "FOREIGN ") || strings.HasPrefix(up, "INDEX") ||
		strings.HasPrefix(up, "KEY ") || strings.HasPrefix(up, "FULLTEXT") ||
		strings.HasPrefix(up, "SPATIAL")
}

func firstIdent(s string) string {
	m := reFirstIdent.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) < 2 {
		return ""
	}
	return strings.Trim(m[1], "`\"")
}

func createTableRef(stmt string) string {
	if n := createTableName(stmt); n != "" {
		return n
	}
	m := reCreateTableRef.FindStringSubmatch(stmt)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func createViewName(stmt string) (name string, materialized bool) {
	m := reCreateViewName.FindStringSubmatch(stmt)
	if len(m) < 3 {
		return "", false
	}
	return strings.TrimSpace(m[2]), strings.TrimSpace(m[1]) != ""
}

func hasNonEmptyInlineComment(def string) bool {
	m := reInlineComment.FindStringSubmatch(def)
	if len(m) < 3 {
		return false
	}
	return strings.TrimSpace(firstNonEmpty(m[1], m[2])) != ""
}

func hasNonEmptyTableComment(stmt string) bool {
	body := tableDefBody(stmt)
	rest := stmt
	if i := strings.Index(stmt, "("); i >= 0 && body != "" {
		if j := strings.Index(stmt[i:], ")"); j >= 0 {
			rest = stmt[i+j+1:]
		}
	}
	m := reTableComment.FindStringSubmatch(rest)
	if len(m) >= 3 && strings.TrimSpace(firstNonEmpty(m[1], m[2])) != "" {
		return true
	}
	return false
}

func normalizeIdent(s string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), "`\""))
}

func normalizeTableRef(s string) string {
	parts := splitQualified(s)
	if len(parts) == 0 {
		return ""
	}
	return normalizeIdent(parts[len(parts)-1])
}

func displayTable(s string) string {
	if n := normalizeTableRef(s); n != "" {
		return n
	}
	return strings.TrimSpace(s)
}

func splitQualified(s string) []string {
	var parts []string
	var cur strings.Builder
	var quote rune
	for _, r := range strings.TrimSpace(s) {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '`':
			quote = r
		case r == '.':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

var (
	reCommentOnColumn = regexp.MustCompile(`(?is)COMMENT\s+ON\s+COLUMN\s+(.+?)\s+IS\s*(?:'((?:[^']|'')*)'|"((?:[^"]|"")*)")`)
	reCommentOnTable  = regexp.MustCompile(`(?is)COMMENT\s+ON\s+TABLE\s+(.+?)\s+IS\s*(?:'((?:[^']|'')*)'|"((?:[^"]|"")*)")`)
	reCommentOnView   = regexp.MustCompile(`(?is)COMMENT\s+ON\s+(MATERIALIZED\s+VIEW|VIEW)\s+(.+?)\s+IS\s*(?:'((?:[^']|'')*)'|"((?:[^"]|"")*)")`)
	reAlterTableName  = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:ONLY\s+)?(.+?)\s+ADD\b`)
	reAddOp           = regexp.MustCompile(`(?i)\bADD\s+`)
	reFirstIdent      = regexp.MustCompile(`^[\s,]*("?[A-Za-z_][\w]*"?|` + "`" + `[^` + "`" + `]+` + "`" + `)`)
	reCreateTableRef  = regexp.MustCompile(`(?is)CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+(.+?)(?:\s*\(|\s+AS\b)`)
	reCreateView      = regexp.MustCompile(`(?i)CREATE\s+(OR\s+REPLACE\s+)?(MATERIALIZED\s+)?VIEW\b`)
	reCreateViewName  = regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?(MATERIALIZED\s+)?VIEW\s+((?:[\w."]+)|(?:"[^"]+"))`)
	reInlineComment   = regexp.MustCompile(`(?i)\bCOMMENT\s*(?:=\s*)?(?:'((?:[^']|'')*)'|"((?:[^"]|"")*)")`)
	reTableComment    = regexp.MustCompile(`(?i)\bCOMMENT\s*(?:=\s*)?(?:'((?:[^']|'')*)'|"((?:[^"]|"")*)")`)
)
