package sqlcheck

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	reCreateIndex      = regexp.MustCompile(`(?i)CREATE\s+(UNIQUE\s+)?INDEX(?:\s+CONCURRENTLY)?(?:\s+IF\s+NOT\s+EXISTS)?\s+["\x60]?([A-Za-z0-9_]+)`)
	reAddUnique        = regexp.MustCompile(`(?i)ADD\s+(?:CONSTRAINT\s+["\x60]?([A-Za-z0-9_]+)["\x60]?\s+)?UNIQUE\b`)
	reAddSerialPK      = regexp.MustCompile(`(?i)ADD\s+(?:COLUMN\s+)?\S+\s+(?:BIG)?SERIAL\b[\s\S]*PRIMARY\s+KEY`)
	reReindex          = regexp.MustCompile(`(?i)^\s*REINDEX\b`)
	reRefreshMat       = regexp.MustCompile(`(?i)REFRESH\s+MATERIALIZED\s+VIEW\b`)
	reCreateWithSchema = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?(?:TABLE|VIEW|MATERIALIZED\s+VIEW|INDEX)\b[\s\S]+?\b([A-Za-z0-9_]+)\s*\.\s*[A-Za-z0-9_"]+`)
	reIndexCols        = regexp.MustCompile(`(?i)(?:INDEX|UNIQUE)\s+[^(]*\(([^)]+)\)`)
	reIdentCharset     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	reSQLKeyword       = regexp.MustCompile(`(?i)^(user|table|index|view|select|insert|update|delete|from|where|order|group|limit|offset|join|left|right|inner|outer|primary|foreign|key|constraint|schema|database|grant|revoke|null|default|check|unique|references|cascade|restrict|session|current)$`)
)

func applyExtraDDLRules(stmt, upper, kw string, dbType string, byName map[string]Rule) []*ruleHit {
	var hits []*ruleHit
	add := func(hit *ruleHit) {
		if hit != nil {
			hits = append(hits, hit)
		}
	}
	isCreate := reCreateTable.MatchString(upper)
	isCTAS := isCreate && reCreateTableAS.MatchString(upper)
	isAddCol := isAlterAddColumn(upper)

	if enabled(byName, "DDLIndexNameSpec") {
		add(checkIndexNameSpec(stmt, upper))
	}
	if enabled(byName, "DDLCreateSpecifySchema") && reCreateBare.MatchString(upper) && !reCreateWithSchema.MatchString(stmt) {
		add(catalogHit("DDLCreateSpecifySchema", "CREATE TABLE/VIEW/INDEX 建议指定 schema"))
	}
	if enabled(byName, "DDLDisallowAddUniqueInline") && reAddUnique.MatchString(upper) {
		add(catalogHit("DDLDisallowAddUniqueInline", "已有表禁止直接 ADD UNIQUE，请先 CREATE UNIQUE INDEX CONCURRENTLY"))
	}
	if enabled(byName, "DDLDisallowAddSerialPK") && reAddSerialPK.MatchString(upper) {
		add(errID("DDLDisallowAddSerialPK", "禁止 ALTER 加 serial/bigserial 主键列"))
	}
	if enabled(byName, "DDLReindexRefreshConcurrently") && (reReindex.MatchString(upper) || reRefreshMat.MatchString(upper)) && !strings.Contains(upper, "CONCURRENTLY") {
		add(catalogHit("DDLReindexRefreshConcurrently", "REINDEX / REFRESH MATERIALIZED VIEW 建议 CONCURRENTLY"))
	}
	if isCreate && !isCTAS && enabled(byName, "DDLRequireUpdateTime") {
		add(checkRequireUpdateTime(stmt, byName))
	}
	if isCreate && !isCTAS && enabled(byName, "DDLForeignKeyPolicy") {
		add(checkForeignKeyPolicy(upper, byName))
	}
	if (isCreate || isAddCol || reCreateIndex.MatchString(upper)) && enabled(byName, "DDLIndexKeyNumberLimit", "DDLIndexNoDuplicateColumn") {
		hits = append(hits, checkIndexColumns(stmt, byName)...)
	}
	if isCreate || isAddCol {
		if enabled(byName, "DDLTableNameSpec") && isCreate {
			add(checkNameSpec(createTableName(stmt), "表名", "DDLTableNameSpec", byName))
		}
		if enabled(byName, "DDLColumnNameSpec") {
			for _, col := range columnNamesOf(stmt, isCreate, isAddCol) {
				add(checkNameSpec(col, "列名", "DDLColumnNameSpec", byName))
			}
		}
		if enabled(byName, "DDLNamingCharset") {
			if isCreate {
				add(checkNamingCharset(createTableName(stmt), "表名"))
			}
			for _, col := range columnNamesOf(stmt, isCreate, isAddCol) {
				add(checkNamingCharset(col, "列名"))
			}
		}
		if enabled(byName, "DDLNamingNoKeyword") {
			if isCreate {
				add(checkNamingKeyword(createTableName(stmt), "表名"))
			}
			for _, col := range columnNamesOf(stmt, isCreate, isAddCol) {
				add(checkNamingKeyword(col, "列名"))
			}
		}
	}
	_ = kw
	_ = dbType
	return hits
}

var reCreateBare = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:UNIQUE\s+)?(?:TABLE|VIEW|MATERIALIZED\s+VIEW|INDEX)\b`)

func checkIndexNameSpec(stmt, upper string) *ruleHit {
	if m := reCreateIndex.FindStringSubmatch(stmt); len(m) >= 3 {
		unique := strings.TrimSpace(m[1]) != ""
		name := strings.ToLower(m[2])
		if unique && !strings.HasPrefix(name, "uk_") {
			return catalogHit("DDLIndexNameSpec", "索引命名规范：唯一索引建议 uk_ 前缀")
		}
		if !unique && !strings.HasPrefix(name, "idx_") && !strings.HasPrefix(name, "uk_") {
			return catalogHit("DDLIndexNameSpec", "索引命名规范：普通索引建议 idx_ 前缀，唯一索引建议 uk_ 前缀")
		}
	}
	if m := reAddUnique.FindStringSubmatch(stmt); len(m) >= 2 {
		name := strings.ToLower(strings.TrimSpace(m[1]))
		if name != "" && !strings.HasPrefix(name, "uk_") && !strings.HasPrefix(name, "idx_") {
			return catalogHit("DDLIndexNameSpec", "索引命名规范：普通索引建议 idx_ 前缀，唯一索引建议 uk_ 前缀")
		}
	}
	_ = upper
	return nil
}

func checkRequireUpdateTime(stmt string, byName map[string]Rule) *ruleHit {
	r, _ := ruleOf(byName, "DDLRequireUpdateTime")
	col := ruleField(r.Value, "column", "update_time")
	if col == "" {
		col = "update_time"
	}
	body := createTableBody(stmt)
	if body == "" {
		return nil
	}
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(col) + `\b`)
	if !re.MatchString(body) {
		return errID("DDLRequireUpdateTime", "建表必须包含时间字段 "+col)
	}
	return nil
}

func checkForeignKeyPolicy(upper string, byName map[string]Rule) *ruleHit {
	r, ok := ruleOf(byName, "DDLForeignKeyPolicy")
	if !ok {
		return nil
	}
	mode := strings.ToLower(ruleField(r.Value, "mode", "off"))
	hasFK := regexp.MustCompile(`(?i)\bREFERENCES\b`).MatchString(upper)
	switch mode {
	case "forbid":
		if hasFK {
			return errID("DDLForeignKeyPolicy", "禁止在建表语句中使用外键")
		}
	case "require":
		if !hasFK {
			return errID("DDLForeignKeyPolicy", "建表必须声明外键")
		}
	}
	return nil
}

func checkIndexColumns(stmt string, byName map[string]Rule) []*ruleHit {
	m := reIndexCols.FindStringSubmatch(stmt)
	if len(m) < 2 {
		return nil
	}
	parts := splitIndexCols(m[1])
	if len(parts) == 0 {
		return nil
	}
	var hits []*ruleHit
	if enabled(byName, "DDLIndexKeyNumberLimit") {
		n, ok := ruleInt(byName, 5, "DDLIndexKeyNumberLimit", "max_keys")
		if ok && n > 0 && len(parts) > n {
			hits = append(hits, errID("DDLIndexKeyNumberLimit", "单索引列数超过上限 "+strconv.Itoa(n)))
		}
	}
	if enabled(byName, "DDLIndexNoDuplicateColumn") {
		seen := map[string]bool{}
		for _, p := range parts {
			k := strings.ToLower(p)
			if seen[k] {
				hits = append(hits, errID("DDLIndexNoDuplicateColumn", "禁止索引列重复"))
				break
			}
			seen[k] = true
		}
	}
	return hits
}

func checkNameSpec(name, kind, rule string, byName map[string]Rule) *ruleHit {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	maxLen := 63
	if n, ok := ruleInt(byName, 63, rule, "max_len"); ok && n > 0 {
		maxLen = n
	}
	if len(name) > maxLen {
		return catalogHit(rule, kind+"长度超过上限 "+strconv.Itoa(maxLen))
	}
	r, _ := ruleOf(byName, rule)
	cs := strings.ToLower(ruleField(r.Value, "case", "any"))
	switch cs {
	case "lower":
		if name != strings.ToLower(name) {
			return catalogHit(rule, kind+"须为小写")
		}
	case "upper":
		if name != strings.ToUpper(name) {
			return catalogHit(rule, kind+"须为大写")
		}
	}
	return nil
}

func checkNamingCharset(name, kind string) *ruleHit {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(name), "pg_") || !reIdentCharset.MatchString(name) {
		return catalogHit("DDLNamingCharset", kind+"须小写字母开头，仅字母数字下划线，且不能使用 pg_ 前缀")
	}
	return nil
}

func checkNamingKeyword(name, kind string) *ruleHit {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if reSQLKeyword.MatchString(name) {
		return catalogHit("DDLNamingNoKeyword", kind+"禁止使用保留字 "+name)
	}
	return nil
}

func checkMergeAlterTable(stmts []string, byName map[string]Rule) *ruleHit {
	if !enabled(byName, "DDLMergeAlterTable") {
		return nil
	}
	seen := map[string]int{}
	for _, stmt := range stmts {
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "ALTER TABLE") {
			continue
		}
		name := strings.ToLower(alterTableName(stmt))
		if name == "" {
			continue
		}
		seen[name]++
		if seen[name] > 1 {
			return catalogHit("DDLMergeAlterTable", "同工单同表多条 ALTER 建议合成一条")
		}
	}
	return nil
}

func columnNamesOf(stmt string, isCreate, isAddCol bool) []string {
	if isCreate {
		return createTableColumns(stmt)
	}
	if isAddCol {
		return alterAddedColumns(stmt)
	}
	return nil
}

func createTableBody(stmt string) string {
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

func splitIndexCols(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"`+"`")
		if f := strings.Fields(p); len(f) > 0 {
			p = f[0]
		}
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// CheckCreateDBWithRules 建库工单：语法门禁之后套用库名前缀等审核规则（不套用「禁止 CREATE DATABASE」）。
func CheckCreateDBWithRules(sqlContent, dbType string, rules []Rule) Result {
	res := CheckCreateDB(sqlContent)
	if !res.OK {
		return res
	}
	byName := indexRules(rules)
	for i, stmt := range SplitSQL(sqlContent) {
		if r, ok := ruleOf(byName, "DDLDatabaseNamePrefix"); ok && ruleOn(r.Value) {
			prefix := rulePrefix(r)
			name := createDBName(stmt)
			if prefix != "" && name != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
				markStmt(&res, i, errID("DDLDatabaseNamePrefix", "库名必须以 "+prefix+" 开头"))
			}
		}
	}
	return res
}
