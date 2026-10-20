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
	OK             bool             `json:"ok"`
	StatementCount int              `json:"statement_count,omitempty"` // len(SplitSQL)，与审核行数可能不同（extras）
	Statements     []StatementRow   `json:"statements"`
	Errors         []map[string]any `json:"errors,omitempty"`
	Warns          []map[string]any `json:"warns,omitempty"`
}

// AttachStatementCount 写入按分号拆分的语句数（与执行侧口径一致）。
func AttachStatementCount(res *Result, sqlContent string) {
	if res == nil {
		return
	}
	n := len(SplitSQL(sqlContent))
	if n == 0 && strings.TrimSpace(sqlContent) != "" {
		// 空拆分但原文非空时仍标 0，便于前端提示
		res.StatementCount = 0
		return
	}
	res.StatementCount = n
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

// bufferIsPendingCTEHead 判断缓冲区是否只有 WITH [RECURSIVE] cte AS (...) 定义、主体还没出现。
func bufferIsPendingCTEHead(buf string) bool {
	names, rest, ok := parseLeadingCTEs(buf)
	return ok && len(names) > 0 && strings.TrimSpace(rest) == ""
}

// parseLeadingCTEs 解析语句开头的 CTE 列表。ok 为 false 表示不是完整的 WITH 头。
func parseLeadingCTEs(stmt string) (names []string, rest string, ok bool) {
	s := strings.TrimSpace(stripSQLComments(stmt))
	i, matched := matchWordAt(s, 0, "WITH")
	if !matched {
		return nil, stmt, false
	}
	if ni, yes := matchWordAt(s, i, "RECURSIVE"); yes {
		i = ni
	}
	for {
		name, ni, yes := readSQLIdent(s, i)
		if !yes || name == "" {
			return nil, stmt, false
		}
		i = ni
		if j := skipSpaceSQL(s, i); j < len(s) && s[j] == '(' {
			if end, closed := skipBalancedParens(s, j); closed {
				if _, asOK := matchWordAt(s, end, "AS"); asOK {
					i = end
				}
			}
		}
		ni, yes = matchWordAt(s, i, "AS")
		if !yes {
			return nil, stmt, false
		}
		i = ni
		if ni, yes = matchWordAt(s, i, "NOT"); yes {
			if ni2, mat := matchWordAt(s, ni, "MATERIALIZED"); mat {
				i = ni2
			}
		} else if ni, yes = matchWordAt(s, i, "MATERIALIZED"); yes {
			i = ni
		}
		end, closed := skipBalancedParens(s, i)
		if !closed {
			return nil, stmt, false
		}
		names = append(names, name)
		i = skipSpaceSQL(s, end)
		if i < len(s) && s[i] == ',' {
			i++
			continue
		}
		return names, strings.TrimSpace(s[i:]), true
	}
}

func skipSpaceSQL(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

func matchWordAt(s string, i int, word string) (int, bool) {
	i = skipSpaceSQL(s, i)
	if i > 0 && isWordByte(s[i-1]) {
		return i, false
	}
	if i+len(word) > len(s) || !strings.EqualFold(s[i:i+len(word)], word) {
		return i, false
	}
	end := i + len(word)
	if end < len(s) && isWordByte(s[end]) {
		return i, false
	}
	return end, true
}

func readSQLIdent(s string, i int) (string, int, bool) {
	i = skipSpaceSQL(s, i)
	if i >= len(s) {
		return "", i, false
	}
	if s[i] == '"' || s[i] == '`' {
		name, j, ok := skipQuotedSQL(s, i, s[i])
		if !ok {
			return "", i, false
		}
		return strings.Trim(name, "\"`"), j, true
	}
	if !isWordByte(s[i]) || (s[i] >= '0' && s[i] <= '9') {
		return "", i, false
	}
	j := i + 1
	for j < len(s) && isWordByte(s[j]) {
		j++
	}
	return s[i:j], j, true
}

// skipQuotedSQL 返回引号内原文（不含两端引号）以及引号后的下标。
func skipQuotedSQL(s string, i int, q byte) (string, int, bool) {
	j := i + 1
	var b strings.Builder
	for j < len(s) {
		if s[j] == q {
			if j+1 < len(s) && s[j+1] == q {
				b.WriteByte(q)
				j += 2
				continue
			}
			return b.String(), j + 1, true
		}
		if q == '\'' && s[j] == '\\' && j+1 < len(s) {
			b.WriteByte(s[j+1])
			j += 2
			continue
		}
		b.WriteByte(s[j])
		j++
	}
	return "", i, false
}

func skipBalancedParens(s string, i int) (int, bool) {
	i = skipSpaceSQL(s, i)
	if i >= len(s) || s[i] != '(' {
		return i, false
	}
	depth := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '"' || c == '`' {
			_, j, ok := skipQuotedSQL(s, i, c)
			if !ok {
				return i, false
			}
			i = j - 1
			continue
		}
		if c == '$' && i+1 < len(s) && s[i+1] == '$' {
			j := i + 2
			for j+1 < len(s) && !(s[j] == '$' && s[j+1] == '$') {
				j++
			}
			if j+1 >= len(s) {
				return i, false
			}
			i = j + 1
			continue
		}
		if c == '(' {
			depth++
			continue
		}
		if c == ')' {
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return i, false
}

// scanTopLevel 在括号和字符串之外回调。返回 true 表示停止。
func scanTopLevel(s string, fn func(i int) bool) {
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '"' || c == '`' {
			_, j, ok := skipQuotedSQL(s, i, c)
			if !ok {
				return
			}
			i = j - 1
			continue
		}
		if c == '$' && i+1 < len(s) && s[i+1] == '$' {
			j := i + 2
			for j+1 < len(s) && !(s[j] == '$' && s[j+1] == '$') {
				j++
			}
			if j+1 >= len(s) {
				return
			}
			i = j + 1
			continue
		}
		if c == '(' {
			depth++
			continue
		}
		if c == ')' {
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 && fn(i) {
			return
		}
	}
}

func splitTopLevelComma(s string) []string {
	var parts []string
	last := 0
	scanTopLevel(s, func(i int) bool {
		if s[i] == ',' {
			parts = append(parts, s[last:i])
			last = i + 1
		}
		return false
	})
	parts = append(parts, s[last:])
	return parts
}

// mainKeyword 返回语句主体关键字。WITH ... UPDATE/INSERT/DELETE/SELECT 返回主体，而不是 WITH。
func mainKeyword(stmt string) string {
	kw := firstKeyword(stmt)
	if kw != "WITH" {
		return kw
	}
	_, rest, ok := parseLeadingCTEs(stmt)
	if !ok || strings.TrimSpace(rest) == "" {
		return kw
	}
	if mk := firstKeyword(rest); mk != "" {
		return mk
	}
	return kw
}

// cteNames 返回本语句 WITH 定义的 CTE 名。
func cteNames(stmt string) []string {
	names, _, ok := parseLeadingCTEs(stmt)
	if !ok {
		return nil
	}
	return names
}

// updateFromOnlyCTEs 判断 UPDATE 顶层 FROM 是否只引用本语句定义的 CTE，且不含 JOIN。
func updateFromOnlyCTEs(stmt string) bool {
	body := stmt
	defined, rest, ok := parseLeadingCTEs(stmt)
	if ok {
		body = rest
	}
	names := map[string]bool{}
	for _, n := range defined {
		names[strings.ToLower(n)] = true
	}
	from, found := topLevelFromList(body)
	if !found || strings.TrimSpace(from) == "" {
		return false
	}
	if containsSQLKeyword(from, "JOIN") {
		return false
	}
	items := splitTopLevelComma(from)
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		table := firstFromTable(item)
		if table == "" || !names[strings.ToLower(table)] {
			return false
		}
	}
	return true
}

func topLevelFromList(body string) (string, bool) {
	start := -1
	scanTopLevel(body, func(i int) bool {
		if ni, ok := matchWordAt(body, i, "FROM"); ok {
			start = ni
			return true
		}
		return false
	})
	if start < 0 {
		return "", false
	}
	end := len(body)
	scanTopLevel(body[start:], func(i int) bool {
		if _, ok := matchWordAt(body[start:], i, "WHERE"); ok {
			end = start + i
			return true
		}
		if _, ok := matchWordAt(body[start:], i, "RETURNING"); ok {
			end = start + i
			return true
		}
		return false
	})
	return strings.TrimSpace(body[start:end]), true
}

func firstFromTable(item string) string {
	s := strings.TrimSpace(item)
	i := 0
	if ni, ok := matchWordAt(s, i, "ONLY"); ok {
		i = ni
	}
	if ni, ok := matchWordAt(s, i, "LATERAL"); ok {
		i = ni
	}
	name, ni, ok := readSQLIdent(s, i)
	if !ok || name == "" {
		return ""
	}
	if j := skipSpaceSQL(s, ni); j < len(s) && s[j] == '.' {
		return ""
	}
	return name
}

// shouldSuppressMissingSemicolonSplit 在 DDL 多行语句内部子句处不强制分段。
func shouldSuppressMissingSemicolonSplit(buf string, keyword string, sqlContent string, i int) bool {
	switch keyword {
	case "INSERT", "UPDATE", "DELETE", "SELECT", "WITH":
		if bufferIsPendingCTEHead(buf) {
			return true
		}
	}
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

// findMissingSemicolonSplits 找出因缺分号而会被 SplitSQL 强制分段的边界摘要（不改变拆句行为）。
func findMissingSemicolonSplits(s string) []string {
	var snippets []string
	var cur strings.Builder
	inSingle := false
	inDollar := false
	parenDepth := 0

	resetBuf := func() { cur.Reset() }

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
					cur.WriteByte(s[i])
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
					i++
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
				resetBuf()
				continue
			}
			if parenDepth == 0 && bufferAtLineStart(cur.String()) && isWordByte(c) {
				kw := matchStatementStartKeyword(s, i)
				if kw != "" && strings.TrimSpace(cur.String()) != "" && !shouldSuppressMissingSemicolonSplit(cur.String(), kw, s, i) {
					prev := strings.TrimSpace(stripSQLComments(cur.String()))
					nextEnd := i
					for nextEnd < len(s) && isWordByte(s[nextEnd]) {
						nextEnd++
					}
					// 再多取一点后文，方便定位
					tail := nextEnd
					for n := 0; tail < len(s) && n < 48; n++ {
						if s[tail] == '\n' {
							break
						}
						tail++
					}
					snippets = append(snippets, missingSemicolonSnippet(prev, s[i:tail]))
					resetBuf()
				}
			}
		}
		cur.WriteByte(c)
	}
	return snippets
}

func missingSemicolonSnippet(prev, next string) string {
	prev = strings.TrimSpace(prev)
	next = strings.TrimSpace(next)
	const max = 72
	if len(prev) > max {
		prev = "…" + prev[len(prev)-max:]
	}
	if len(next) > max {
		next = next[:max] + "…"
	}
	return prev + " ←缺少;→ " + next
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
	res.StatementCount = len(stmts)
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
