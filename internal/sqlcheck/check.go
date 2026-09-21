package sqlcheck

import (
	"regexp"
	"strings"
)

type StatementRow struct {
	Stage      string `json:"stage"`
	ErrorLevel string `json:"error_level"`
	Message    string `json:"message"`
	SQL        string `json:"sql"`
	RuleID     string `json:"rule_id,omitempty"`
}

type Result struct {
	OK         bool             `json:"ok"`
	Statements []StatementRow   `json:"statements"`
	Errors     []map[string]any `json:"errors,omitempty"`
	Warns      []map[string]any `json:"warns,omitempty"`
}

var (
	reCreateDB = regexp.MustCompile(`(?is)^\s*CREATE\s+DATABASE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:` + "`" + `([^` + "`" + `]+)` + "`" + `|"([^"]+)"|(\w+))`)
	reWhere    = regexp.MustCompile(`(?is)\bWHERE\b`)
)

// stripSQLComments 去掉 -- / # 行注释与 /* */ 块注释（单引号、$$ 块内不处理）。
func stripSQLComments(sql string) string {
	var b strings.Builder
	inSingle := false
	inDollar := false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if !inDollar && c == '\'' && (i == 0 || sql[i-1] != '\\') {
			inSingle = !inSingle
			b.WriteByte(c)
			continue
		}
		if !inSingle && i+1 < len(sql) && c == '$' && sql[i+1] == '$' {
			inDollar = !inDollar
			b.WriteByte(c)
			b.WriteByte(sql[i+1])
			i++
			continue
		}
		if !inSingle && !inDollar {
			if c == '-' && i+1 < len(sql) && sql[i+1] == '-' {
				i += 2
				for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
					i++
				}
				if i < len(sql) {
					b.WriteByte(sql[i])
				}
				continue
			}
			if c == '#' {
				i++
				for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
					i++
				}
				if i < len(sql) {
					b.WriteByte(sql[i])
				}
				continue
			}
			if c == '/' && i+1 < len(sql) && sql[i+1] == '*' {
				i += 2
				for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
					i++
				}
				if i+1 < len(sql) {
					i++
				}
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// ddlStatementStartKeywords 为可靠的语句起始关键字，用于在缺分号时识别新语句边界。
var ddlStatementStartKeywords = map[string]bool{
	"CREATE": true, "COMMENT": true, "REINDEX": true, "REFRESH": true,
	"TRUNCATE": true, "GRANT": true, "REVOKE": true,
	"INSERT": true, "UPDATE": true, "DELETE": true, "SELECT": true,
	"WITH": true, "DO": true, "ALTER": true, "DROP": true,
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func bufferAtLineStart(b string) bool {
	if idx := strings.LastIndexByte(b, '\n'); idx >= 0 {
		b = b[idx+1:]
	}
	return strings.TrimSpace(b) == ""
}

func bufferEndsWithPendingQueryAS(buf string) bool {
	s := strings.TrimSpace(buf)
	if s == "" {
		return false
	}
	i := len(s) - 1
	for i >= 0 && isWordByte(s[i]) {
		i--
	}
	return strings.EqualFold(strings.TrimSpace(s[i+1:]), "AS")
}

func bufferEndsWithClosedCTE(buf string) bool {
	s := strings.TrimSpace(buf)
	if s == "" || s[len(s)-1] != ')' {
		return false
	}
	return containsSQLKeyword(s, "WITH")
}

// bufferEndsWithSetOp 判断 buffer 是否以 UNION / EXCEPT / INTERSECT（可带 ALL）结尾。
func bufferEndsWithSetOp(buf string) bool {
	fields := strings.Fields(strings.ToUpper(strings.TrimSpace(buf)))
	n := len(fields)
	if n == 0 {
		return false
	}
	last := fields[n-1]
	if last == "ALL" && n >= 2 {
		switch fields[n-2] {
		case "UNION", "EXCEPT", "INTERSECT":
			return true
		}
		return false
	}
	switch last {
	case "UNION", "EXCEPT", "INTERSECT":
		return true
	}
	return false
}

func containsSQLKeyword(s, kw string) bool {
	upper := strings.ToUpper(s)
	kw = strings.ToUpper(kw)
	n := len(kw)
	for i := 0; i+n <= len(upper); i++ {
		if upper[i:i+n] != kw {
			continue
		}
		if i > 0 && isWordByte(upper[i-1]) {
			continue
		}
		if i+n < len(upper) && isWordByte(upper[i+n]) {
			continue
		}
		return true
	}
	return false
}

func bufferInPendingCreateMaterializedView(buf string) bool {
	upper := strings.ToUpper(strings.TrimSpace(buf))
	if !strings.Contains(upper, "CREATE MATERIALIZED VIEW") {
		return false
	}
	return !strings.Contains(upper, " AS") && !strings.HasSuffix(upper, " AS")
}

// shouldSuppressMissingSemicolonSplit 在 DDL 多行语句内部子句处不强制分段。
func shouldSuppressMissingSemicolonSplit(buf string, keyword string, sqlContent string, i int) bool {
	if keyword == "SELECT" || keyword == "WITH" {
		return bufferEndsWithPendingQueryAS(buf) || bufferEndsWithClosedCTE(buf) || bufferEndsWithSetOp(buf)
	}
	if keyword == "REFRESH" {
		if bufferInPendingCreateMaterializedView(buf) {
			return true
		}
		j := i
		for j < len(sqlContent) && isWordByte(sqlContent[j]) {
			j++
		}
		rest := strings.ToUpper(strings.TrimSpace(sqlContent[j:]))
		switch {
		case strings.HasPrefix(rest, "MANUAL"),
			strings.HasPrefix(rest, "ASYNC"),
			strings.HasPrefix(rest, "IMMEDIATE"),
			strings.HasPrefix(rest, "DEFERRED"),
			strings.HasPrefix(rest, "INCREMENTAL"):
			return true
		}
	}
	return false
}

func matchStatementStartKeyword(s string, i int) string {
	j := i
	for j < len(s) && isWordByte(s[j]) {
		j++
	}
	if j == i {
		return ""
	}
	word := strings.ToUpper(s[i:j])
	if word == "COMMENT" {
		rest := strings.TrimSpace(s[j:])
		if !strings.HasPrefix(strings.ToUpper(rest), "ON") {
			return ""
		}
	}
	if ddlStatementStartKeywords[word] {
		return word
	}
	return ""
}

// SplitSQL 按分号拆分；忽略字符串、$$ 块、注释内的分号，以及括号内的分号；丢弃纯注释段。
// 当行首出现语句起始关键字而上一条缺少分号时强制分段，但在 UNION/EXCEPT/INTERSECT、
// CREATE VIEW AS\nSELECT、CTE 后续 SELECT 等处抑制误拆（对齐 hub）。
func SplitSQL(s string) []string {
	var out []string
	var cur strings.Builder
	inSingle := false
	inDollar := false
	parenDepth := 0

	flush := func() {
		raw := strings.TrimSpace(stripSQLComments(cur.String()))
		cur.Reset()
		if raw == "" {
			return
		}
		out = append(out, raw)
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inDollar && c == '\'' && (i == 0 || s[i-1] != '\\') {
			inSingle = !inSingle
			cur.WriteByte(c)
			continue
		}
		if !inSingle && i+1 < len(s) && c == '$' && s[i+1] == '$' {
			inDollar = !inDollar
			cur.WriteByte(c)
			cur.WriteByte(s[i+1])
			i++
			continue
		}
		if !inSingle && !inDollar {
			if c == '-' && i+1 < len(s) && s[i+1] == '-' {
				i += 2
				for i < len(s) && s[i] != '\n' && s[i] != '\r' {
					i++
				}
				if i < len(s) {
					cur.WriteByte(s[i]) // 保留换行，供行首关键字检测
				}
				continue
			}
			if c == '#' {
				i++
				for i < len(s) && s[i] != '\n' && s[i] != '\r' {
					i++
				}
				if i < len(s) {
					cur.WriteByte(s[i])
				}
				continue
			}
			if c == '/' && i+1 < len(s) && s[i+1] == '*' {
				i += 2
				for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
					i++
				}
				if i+1 < len(s) {
					i++ // skip '/'
				}
				continue
			}
			if c == '(' {
				parenDepth++
				cur.WriteByte(c)
				continue
			}
			if c == ')' {
				if parenDepth > 0 {
					parenDepth--
				}
				cur.WriteByte(c)
				continue
			}
			if c == ';' && parenDepth == 0 {
				flush()
				continue
			}
			if parenDepth == 0 && bufferAtLineStart(cur.String()) && isWordByte(c) {
				kw := matchStatementStartKeyword(s, i)
				if kw != "" && strings.TrimSpace(cur.String()) != "" && !shouldSuppressMissingSemicolonSplit(cur.String(), kw, s, i) {
					flush()
				}
			}
		}
		cur.WriteByte(c)
	}
	flush()
	return out
}

func firstKeyword(sql string) string {
	sql = strings.TrimSpace(stripSQLComments(sql))
	if sql == "" {
		return ""
	}
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToUpper(fields[0])
}

func ParseCreateDBNames(sql string) []string {
	var names []string
	for _, stmt := range SplitSQL(sql) {
		m := reCreateDB.FindStringSubmatch(stmt)
		if len(m) == 0 {
			continue
		}
		for i := 1; i < len(m); i++ {
			if strings.TrimSpace(m[i]) != "" {
				names = append(names, strings.TrimSpace(m[i]))
				break
			}
		}
	}
	return names
}

func CheckTicket(sqlContent, issueType string, forbidDropDB, requireWhere bool) Result {
	return CheckTicketWithRules(sqlContent, issueType, "", LegacyRules(forbidDropDB, requireWhere))
}

func CheckCreateDB(sqlContent string) Result {
	res := Result{OK: true}
	stmts := SplitSQL(sqlContent)
	if len(stmts) == 0 {
		return fail("parse", "SQL 不能为空", sqlContent)
	}
	for _, stmt := range stmts {
		row := StatementRow{Stage: "parse", ErrorLevel: "INFO", Message: "CREATE DATABASE 语法通过", SQL: stmt}
		if !reCreateDB.MatchString(stmt) {
			row.ErrorLevel, row.Message, res.OK = "ERROR", "仅允许 CREATE DATABASE 语句", false
		}
		res.Statements = append(res.Statements, row)
		if row.ErrorLevel == "ERROR" {
			res.Errors = append(res.Errors, map[string]any{"message": row.Message, "sql": stmt})
		}
	}
	return res
}

func isDDL(kw string) bool {
	switch kw {
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME", "COMMENT":
		return true
	default:
		return false
	}
}

func isDML(kw string) bool {
	switch kw {
	case "INSERT", "UPDATE", "DELETE", "REPLACE":
		return true
	default:
		return false
	}
}

func fail(stage, msg, sql string) Result {
	return Result{
		OK: false,
		Statements: []StatementRow{{
			Stage: stage, ErrorLevel: "ERROR", Message: msg, SQL: sql,
		}},
		Errors: []map[string]any{{"message": msg, "sql": sql}},
	}
}
