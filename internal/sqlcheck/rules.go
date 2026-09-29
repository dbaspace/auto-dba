package sqlcheck

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

type Rule struct {
	Name  string
	Value string
	Label string
}

type ruleHit struct {
	Level   string
	Message string
	RuleID  string
}

func errID(id, msg string) *ruleHit  { return &ruleHit{Level: "ERROR", Message: msg, RuleID: id} }
func warnID(id, msg string) *ruleHit { return &ruleHit{Level: "WARN", Message: msg, RuleID: id} }

func catalogHit(id, msg string) *ruleHit {
	if warnRuleIDs[id] {
		return warnID(id, msg)
	}
	return errID(id, msg)
}

var warnRuleIDs = map[string]bool{
	"DMLAllowSubquery":              true,
	"DMLAllowMultiTable":            true,
	"DMLInsertDisallowSelectStar":   true,
	"DDLIndexCreateConcurrently":    true,
	"DDLReindexRefreshConcurrently": true,
	"DDLBackwardIncompatible":       true,
	"DDLColumnTypeChange":           true,
	"DDLCreateSpecifySchema":        true,
	"DDLMergeAlterTable":            true,
	"DDLIndexNameSpec":              true,
	"DDLTableNameSpec":              true,
	"DDLColumnNameSpec":             true,
	"DDLNamingNoKeyword":            true,
	"DDLNamingCharset":              true,
	"DDLDisallowAddUniqueInline":    true,
}

func LegacyRules(forbidDropDB, requireWhere bool) []Rule {
	var out []Rule
	if forbidDropDB {
		out = append(out,
			Rule{Name: "disable_drop_database", Value: "true"},
			Rule{Name: "DDLForbidDropDatabase", Value: "false"},
		)
	}
	if requireWhere {
		out = append(out,
			Rule{Name: "check_dml_where", Value: "true"},
			Rule{Name: "DMLWhere", Value: "true"},
			Rule{Name: "DMLRequireWhere", Value: "true"},
		)
	}
	return out
}

func CheckTicketWithRules(sqlContent, issueType, dbType string, rules []Rule) Result {
	res := Result{OK: true}
	stmts := SplitSQL(sqlContent)
	if len(stmts) == 0 {
		return fail("empty", "SQL 不能为空", sqlContent)
	}
	byName := indexRules(rules)
	dbType = strings.ToLower(strings.TrimSpace(dbType))
	if dbType == "postgresql" || dbType == "pg" {
		dbType = "postgres"
	}
	if dbType == "sr" {
		dbType = "starrocks"
	}
	var extras []StatementRow
	for _, stmt := range stmts {
		row := StatementRow{Stage: "static", ErrorLevel: "INFO", Message: "通过", SQL: stmt}
		hits := checkStatement(stmt, issueType, dbType, byName)
		for i, hit := range hits {
			if hit == nil {
				continue
			}
			if i == 0 {
				row.ErrorLevel, row.Message, row.RuleID = hit.Level, hit.Message, hit.RuleID
			} else {
				extras = append(extras, StatementRow{Stage: "static", ErrorLevel: hit.Level, Message: hit.Message, SQL: stmt, RuleID: hit.RuleID})
			}
			appendHit(&res, stmt, hit)
		}
		res.Statements = append(res.Statements, row)
	}
	if strings.EqualFold(issueType, "ddl") {
		applyTicketCommentRules(stmts, dbType, byName, &res)
		if dbType == "postgres" {
			applyRequireUpdateTimeRules(stmts, byName, &res)
		}
		if hit := checkMergeAlterTable(stmts, byName); hit != nil {
			appendHit(&res, stmts[0], hit)
			extras = append(extras, StatementRow{Stage: "static", ErrorLevel: hit.Level, Message: hit.Message, SQL: stmts[0], RuleID: hit.RuleID})
		}
	}
	res.Statements = append(res.Statements, extras...)
	return res
}

func appendHit(res *Result, stmt string, hit *ruleHit) {
	if res == nil || hit == nil {
		return
	}
	item := map[string]any{"message": hit.Message, "sql": stmt, "rule_id": hit.RuleID}
	if hit.Level == "ERROR" {
		res.OK = false
		res.Errors = append(res.Errors, item)
		return
	}
	if hit.Level == "WARN" {
		res.Warns = append(res.Warns, item)
	}
}

func indexRules(rules []Rule) map[string]Rule {
	out := map[string]Rule{}
	for _, r := range rules {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		out[strings.ToLower(name)] = r
	}
	return out
}

func ruleOf(byName map[string]Rule, names ...string) (Rule, bool) {
	for _, n := range names {
		if r, ok := byName[strings.ToLower(n)]; ok {
			return r, true
		}
	}
	return Rule{}, false
}

func checkStatement(stmt, issueType, dbType string, byName map[string]Rule) []*ruleHit {
	kw := firstKeyword(stmt)
	upper := strings.ToUpper(stmt)
	switch issueType {
	case "ddl":
		if !isDDL(kw) && kw != "GRANT" && kw != "REVOKE" && kw != "LOCK" && kw != "VACUUM" && kw != "CLUSTER" && kw != "COMMENT" && kw != "REINDEX" && kw != "REFRESH" {
			return []*ruleHit{errID("TicketType", "语句必须是 DDL（CREATE/ALTER/DROP/TRUNCATE/RENAME/COMMENT）")}
		}
		if kw == "REVOKE" || reRevoke.MatchString(upper) {
			return []*ruleHit{errID("ForbidRevoke", "不允许在工单中执行 REVOKE（权限回收）语句。")}
		}
		if reCreateDatabase.MatchString(upper) && !hasRule(byName, "DDLForbidCreateDatabase") {
			return []*ruleHit{errID("DDLForbidCreateDatabase", "CREATE DATABASE 请走「新增数据库」工单")}
		}
	case "dml":
		if isDDL(kw) {
			if enabled(byName, "DMLForbidDDL") || !hasRule(byName, "DMLForbidDDL") {
				return []*ruleHit{errID("DMLForbidDDL", "DML 工单仅支持 DML 语句，禁止提交 DDL，请使用 DDL 工单")}
			}
		}
		if !isDML(kw) && kw != "GRANT" && kw != "REVOKE" && kw != "LOCK" && kw != "VACUUM" {
			return []*ruleHit{errID("TicketType", "语句必须是 DML（INSERT/UPDATE/DELETE）")}
		}
		if kw == "REVOKE" || reRevoke.MatchString(upper) {
			return []*ruleHit{errID("ForbidRevoke", "不允许在工单中执行 REVOKE（权限回收）语句。")}
		}
	case "dcl":
		if kw != "GRANT" && kw != "REVOKE" {
			return []*ruleHit{errID("TicketType", "语句必须是 GRANT/REVOKE")}
		}
		return nil
	case "dql":
		if kw != "SELECT" && kw != "WITH" && kw != "EXPLAIN" && kw != "SHOW" && kw != "DESC" && kw != "DESCRIBE" {
			return []*ruleHit{errID("TicketType", "语句必须是 SELECT/WITH/SHOW/EXPLAIN")}
		}
		return nil
	}
	if issueType != "ddl" && issueType != "dml" {
		return nil
	}
	return applyCatalogRules(stmt, upper, kw, issueType, dbType, byName)
}

func hasRule(byName map[string]Rule, name string) bool {
	_, ok := byName[strings.ToLower(name)]
	return ok
}

func enabled(byName map[string]Rule, names ...string) bool {
	r, ok := ruleOf(byName, names...)
	if !ok {
		return false
	}
	if isAllowRule(r.Name) {
		return !ruleAllow(r.Value)
	}
	return ruleOn(r.Value)
}

func isAllowRule(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "ddlforbidcreatedatabase", "ddlforbiddropdatabase", "ddlforbidtruncate", "ddlforbiddropschema",
		"ddlallowdropcolumn", "dmlallowsubquery", "dmlallowmultitable":
		return true
	default:
		return false
	}
}

func applyCatalogRules(stmt, upper, kw, issueType, dbType string, byName map[string]Rule) []*ruleHit {
	var hits []*ruleHit
	if enabled(byName, "DDLForbidGrant") && reGrant.MatchString(upper) {
		hits = append(hits, errID("DDLForbidGrant", ruleMsg(byName, "DDLForbidGrant", "禁止在工单中执行 GRANT")))
	}
	if dbType == "postgres" && enabled(byName, "DDLForbidLockVacuumFull") && (reLockTable.MatchString(upper) || reVacuumFull.MatchString(upper)) {
		hits = append(hits, errID("DDLForbidLockVacuumFull", ruleMsg(byName, "DDLForbidLockVacuumFull", "禁止在工单中执行 LOCK TABLE / VACUUM FULL")))
	}
	if dbType == "postgres" && enabled(byName, "DDLForbidCluster") && reCluster.MatchString(upper) {
		hits = append(hits, errID("DDLForbidCluster", ruleMsg(byName, "DDLForbidCluster", "禁止在工单中执行 CLUSTER")))
	}
	if issueType == "ddl" {
		hits = append(hits, applyDDLRules(stmt, upper, kw, dbType, byName)...)
	}
	if issueType == "dml" {
		hits = append(hits, applyDMLRules(stmt, upper, kw, byName)...)
	}
	return hits
}

func applyDDLRules(stmt, upper, kw, dbType string, byName map[string]Rule) []*ruleHit {
	var hits []*ruleHit
	add := func(hit *ruleHit) {
		if hit != nil {
			hits = append(hits, hit)
		}
	}
	if enabled(byName, "disable_drop_database") && reDropDatabase.MatchString(upper) {
		add(errID("disable_drop_database", ruleMsg(byName, "disable_drop_database", "已开启约束：禁止 DROP DATABASE")))
	}
	if r, ok := ruleOf(byName, "enable_drop_database"); ok && !ruleOn(r.Value) && reDropDatabase.MatchString(upper) {
		add(errID("enable_drop_database", "已开启约束：禁止 DROP DATABASE"))
	}
	if enabled(byName, "DDLForbidDropDatabase") && reDropDatabase.MatchString(upper) {
		add(errID("DDLForbidDropDatabase", ruleMsg(byName, "DDLForbidDropDatabase", "禁止在工单中执行 DROP DATABASE")))
	}
	if enabled(byName, "DDLForbidCreateDatabase") && reCreateDatabase.MatchString(upper) {
		add(errID("DDLForbidCreateDatabase", ruleMsg(byName, "DDLForbidCreateDatabase", "禁止在工单中执行 CREATE DATABASE")))
	}
	if enabled(byName, "DDLForbidTruncate") && reTruncate.MatchString(upper) {
		add(errID("DDLForbidTruncate", ruleMsg(byName, "DDLForbidTruncate", "禁止在工单中执行 TRUNCATE")))
	}
	if enabled(byName, "DDLForbidDropSchema") && reDropSchema.MatchString(upper) {
		add(errID("DDLForbidDropSchema", ruleMsg(byName, "DDLForbidDropSchema", "禁止在工单中执行 DROP SCHEMA")))
	}
	if r, ok := ruleOf(byName, "enable_drop_table"); ok && !ruleOn(r.Value) && reDropTable.MatchString(upper) {
		add(errID("enable_drop_table", "已开启约束：禁止 DROP TABLE"))
	}
	add(checkDropTableRule(stmt, upper, byName))
	if enabled(byName, "DDLAllowDropColumn") && reDropColumn.MatchString(upper) {
		add(errID("DDLAllowDropColumn", ruleMsg(byName, "DDLAllowDropColumn", "禁止 DROP COLUMN")))
	}
	if !enabled(byName, "DDLForbidCreateDatabase") && reCreateDatabase.MatchString(upper) {
		if r, ok := ruleOf(byName, "DDLDatabaseNamePrefix"); ok && ruleOn(r.Value) {
			prefix := rulePrefix(r)
			name := createDBName(stmt)
			if prefix != "" && name != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
				add(errID("DDLDatabaseNamePrefix", "库名必须以 "+prefix+" 开头"))
			}
		}
	}

	isCreate := reCreateTable.MatchString(upper)
	isCTAS := isCreate && reCreateTableAS.MatchString(upper)
	isAddCol := isAlterAddColumn(upper)
	if isCreate {
		if r, ok := ruleOf(byName, "DDLTableNamePrefix"); ok && ruleOn(r.Value) {
			prefix := rulePrefix(r)
			name := createTableName(stmt)
			if prefix != "" && name != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
				add(errID("DDLTableNamePrefix", "表名必须以 "+prefix+" 开头"))
			}
		}
		if !isCTAS {
			if enabled(byName, "check_primary_key", "DDLCheckPrimaryKeyExists", "DDLRequirePrimaryKey") && !hasPrimaryKey(upper, dbType) {
				add(errID("DDLCheckPrimaryKeyExists", ruleMsg(byName, "DDLCheckPrimaryKeyExists", "建表必须拥有主键")))
			}
			if dbType == "starrocks" && enabled(byName, "DDLEnablePrimaryKey") && !reStarRocksKey.MatchString(upper) {
				add(errID("DDLEnablePrimaryKey", ruleMsg(byName, "DDLEnablePrimaryKey", "建表必须声明 Key（PRIMARY/UNIQUE/DUPLICATE/AGGREGATE KEY）")))
			}
			if n, ok := ruleInt(byName, 64, "DDLMaxColumnCount", "max_columns"); ok && enabled(byName, "DDLMaxColumnCount") {
				if cols := countCreateTableColumns(stmt); cols > n {
					add(errID("DDLMaxColumnCount", "建表列数超过上限 "+strconv.Itoa(n)))
				}
			}
		}
	}
	if isAddCol {
		if enabled(byName, "DDLDisallowAddColumnWithDefault") && reDefault.MatchString(stmt) && !isConstantDefault(stmt) {
			add(errID("DDLDisallowAddColumnWithDefault", "禁止 ADD COLUMN 使用非常量 DEFAULT（会重写表）"))
		}
		if enabled(byName, "DDLAddColumnNotNullRequireDefault") && reNotNull.MatchString(stmt) && !reDefault.MatchString(stmt) {
			add(errID("DDLAddColumnNotNullRequireDefault", "ADD COLUMN NOT NULL 必须带 DEFAULT"))
		}
		if enabled(byName, "DDLAddColumnRequireDefault") && !reDefault.MatchString(stmt) {
			add(errID("DDLAddColumnRequireDefault", "ALTER 加列必须有 DEFAULT"))
		}
		if (enabled(byName, "DDLCheckColumnNullable") || mysqlRequireNotNull(byName)) && !reNotNull.MatchString(stmt) {
			add(errID("DDLCheckColumnNullable", ruleMsg(byName, "DDLCheckColumnNullable", "列必须为 NOT NULL")))
		}
		if enabled(byName, "DDLCheckColumnDefault") && !reDefault.MatchString(stmt) {
			add(errID("DDLCheckColumnDefault", ruleMsg(byName, "DDLCheckColumnDefault", "列必须拥有 DEFAULT")))
		}
	}
	if isCreate || isAddCol {
		if enabled(byName, "DDLCheckFloatDouble") && reFloatDouble.MatchString(upper) {
			add(errID("DDLCheckFloatDouble", ruleMsg(byName, "DDLCheckFloatDouble", "禁止使用 FLOAT/DOUBLE，请改用 DECIMAL")))
		}
		if enabled(byName, "DDLColumnTypeDisallow") && hasDisallowedType(stmt, byName) {
			add(errID("DDLColumnTypeDisallow", ruleMsg(byName, "DDLColumnTypeDisallow", "禁止使用配置的列类型")))
		}
		if r, ok := ruleOf(byName, "enable_blob_type"); ok && !ruleOn(r.Value) && reBlob.MatchString(upper) {
			add(errID("enable_blob_type", "不允许使用 BLOB 类型"))
		}
		if r, ok := ruleOf(byName, "enable_timestamp_type"); ok && !ruleOn(r.Value) && reTimestamp.MatchString(upper) {
			add(errID("enable_timestamp_type", "不允许使用 TIMESTAMP 类型"))
		}
		add(checkCharLength(stmt, byName))
	}
	if isCreate && dbType != "postgres" && enabled(byName, "check_table_comment", "DDLCheckTableComment") && !hasComment(stmt, upper, dbType) {
		add(errID("DDLCheckTableComment", ruleMsg(byName, "DDLCheckTableComment", "强制表必须拥有表注释")))
	}
	if enabled(byName, "DDLDisallowAddNotNull") && reSetNotNull.MatchString(upper) {
		add(errID("DDLDisallowAddNotNull", "禁止给已有列加 NOT NULL"))
	}
	if enabled(byName, "DDLAddCheckNotValid") && reAddCheck.MatchString(upper) && !strings.Contains(upper, "NOT VALID") {
		add(errID("DDLAddCheckNotValid", "ADD CHECK 须带 NOT VALID"))
	}
	if enabled(byName, "DDLAddFKNotValid") && reAddFK.MatchString(upper) && !strings.Contains(upper, "NOT VALID") {
		add(errID("DDLAddFKNotValid", "ADD FOREIGN KEY 须带 NOT VALID"))
	}
	if enabled(byName, "DDLIndexCreateConcurrently") && reIndexDDL.MatchString(upper) && !strings.Contains(upper, "CONCURRENTLY") {
		add(catalogHit("DDLIndexCreateConcurrently", "索引 CREATE/DROP 建议 CONCURRENTLY"))
	}
	if enabled(byName, "DDLBackwardIncompatible") && (reDropColumn.MatchString(upper) || reRename.MatchString(upper)) {
		add(catalogHit("DDLBackwardIncompatible", "后向不兼容 DDL（DROP/RENAME）"))
	}
	if enabled(byName, "DDLColumnTypeChange") && reAlterType.MatchString(upper) {
		add(catalogHit("DDLColumnTypeChange", "列类型变更需确认兼容性"))
	}
	hits = append(hits, applyExtraDDLRules(stmt, upper, kw, dbType, byName)...)
	return hits
}

func applyDMLRules(stmt, upper, kw string, byName map[string]Rule) []*ruleHit {
	var hits []*ruleHit
	add := func(hit *ruleHit) {
		if hit != nil {
			hits = append(hits, hit)
		}
	}
	if (kw == "UPDATE" || kw == "DELETE") && enabled(byName, "check_dml_where", "DMLWhere", "DMLRequireWhere") {
		if !reWhere.MatchString(stmt) {
			add(errID("DMLWhere", ruleMsg(byName, "DMLWhere", "UPDATE/DELETE 必须带 WHERE")))
		} else if taut := tautologyHit(stmt, byName); taut != "" {
			add(errID("DMLWhere", taut))
		}
	}
	if kw == "INSERT" && enabled(byName, "check_insert_field", "DMLInsertMustSpecifyColumn") && !reInsertCols.MatchString(stmt) {
		add(errID("DMLInsertMustSpecifyColumn", ruleMsg(byName, "DMLInsertMustSpecifyColumn", "INSERT 必须指定列清单")))
	}
	if enabled(byName, "DMLInsertDisallowSelectStar") && reInsertSelectStar.MatchString(upper) {
		add(catalogHit("DMLInsertDisallowSelectStar", ruleMsg(byName, "DMLInsertDisallowSelectStar", "禁止 INSERT ... SELECT *")))
	}
	if enabled(byName, "DMLForbidSelectStar") && reSelectStar.MatchString(upper) {
		add(errID("DMLForbidSelectStar", ruleMsg(byName, "DMLForbidSelectStar", "禁止 SELECT *")))
	}
	if enabled(byName, "DMLInsertDisallowOrderByRand") && reOrderByRand.MatchString(upper) {
		add(errID("DMLInsertDisallowOrderByRand", ruleMsg(byName, "DMLInsertDisallowOrderByRand", "禁止 INSERT ... ORDER BY random()/rand()")))
	}
	if enabled(byName, "DMLAllowSubquery") && (kw == "UPDATE" || kw == "DELETE") && reHasSelect.MatchString(upper) {
		add(catalogHit("DMLAllowSubquery", ruleMsg(byName, "DMLAllowSubquery", "不允许包含子查询")))
	}
	if enabled(byName, "DMLAllowMultiTable") && isMultiTableDML(kw, upper) {
		add(catalogHit("DMLAllowMultiTable", "不允许一次更新/删除多表"))
	}
	if kw == "UPDATE" && enabled(byName, "DMLUpdateSetAnd") && updateSetUsesAnd(stmt) {
		add(errID("DMLUpdateSetAnd", ruleMsg(byName, "DMLUpdateSetAnd", "UPDATE SET 多个赋值应使用逗号分隔，不能使用 AND")))
	}
	if kw == "INSERT" {
		if n, ok := ruleInt(byName, 0, "DMLInsertRowLimit", "DMLMaxAffectRows", "max_rows"); ok && n > 0 && enabled(byName, "DMLInsertRowLimit", "DMLMaxAffectRows") {
			if rows := countInsertRows(stmt); rows > n {
				add(errID("DMLInsertRowLimit", "INSERT 行数超过上限 "+strconv.Itoa(n)))
			}
		}
	}
	return hits
}

func mysqlRequireNotNull(byName map[string]Rule) bool {
	r, ok := ruleOf(byName, "enable_nullable")
	return ok && !ruleOn(r.Value)
}

func checkDropTableRule(stmt, upper string, byName map[string]Rule) *ruleHit {
	if !reDropTable.MatchString(upper) {
		return nil
	}
	if enabled(byName, "DDLForbidDropTable") {
		r, _ := ruleOf(byName, "DDLForbidDropTable")
		mode := strings.ToLower(ruleField(r.Value, "mode", "forbid"))
		if mode == "require_del_suffix" {
			suffix := ruleField(r.Value, "suffix", "_del")
			name := dropTableName(stmt)
			if name != "" && !strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
				return errID("DDLForbidDropTable", "DROP TABLE 仅允许删除以 "+suffix+" 结尾的表")
			}
			return nil
		}
		return errID("DDLForbidDropTable", ruleMsg(byName, "DDLForbidDropTable", "禁止在工单中执行 DROP TABLE"))
	}
	return nil
}

func checkCharLength(stmt string, byName map[string]Rule) *ruleHit {
	maxChar, maxVarchar := 0, 0
	if enabled(byName, "DDLCharVarcharLength") {
		if ruleUnlimited(byName, "DDLCharVarcharLength") {
			return nil
		}
		if n, ok := ruleInt(byName, 0, "DDLCharVarcharLength", "max_char"); ok {
			maxChar = n
		}
		if n, ok := ruleInt(byName, 0, "DDLCharVarcharLength", "max_varchar"); ok {
			maxVarchar = n
		}
	}
	if n, ok := ruleInt(byName, 0, "max_char_length"); ok && enabled(byName, "max_char_length") {
		maxChar = n
	}
	if n, ok := ruleInt(byName, 0, "max_varchar_length"); ok && enabled(byName, "max_varchar_length") {
		maxVarchar = n
	}
	if maxVarchar > 0 {
		for _, m := range reVarcharLen.FindAllStringSubmatch(stmt, -1) {
			if n, _ := strconv.Atoi(m[1]); n > maxVarchar {
				return errID("DDLCharVarcharLength", "VARCHAR 长度超过上限 "+strconv.Itoa(maxVarchar))
			}
		}
	}
	if maxChar > 0 {
		for _, m := range reCharLen.FindAllStringSubmatch(stmt, -1) {
			if n, _ := strconv.Atoi(m[1]); n > maxChar {
				return errID("DDLCharVarcharLength", "CHAR 长度超过上限 "+strconv.Itoa(maxChar))
			}
		}
	}
	return nil
}

func hasComment(stmt, upper, dbType string) bool {
	if reCommentMySQL.MatchString(stmt) || reCommentPG.MatchString(stmt) {
		return true
	}
	if dbType == "postgres" && strings.Contains(upper, "COMMENT") && strings.Contains(upper, "ON") && strings.Contains(upper, "IS") {
		return true
	}
	return false
}

func hasPrimaryKey(upper, dbType string) bool {
	if rePrimaryKey.MatchString(upper) {
		return true
	}
	if dbType == "starrocks" && reStarRocksKey.MatchString(upper) {
		return true
	}
	return false
}

func tautologyHit(stmt string, byName map[string]Rule) string {
	r, ok := ruleOf(byName, "DMLWhere", "DMLRequireWhere")
	if !ok {
		return ""
	}
	vals := ruleStringSlice(r.Value, "tautologies", nil)
	if len(vals) == 0 {
		vals = splitCSV(r.Value)
	}
	if len(vals) == 0 || (len(vals) == 1 && (vals[0] == "true" || vals[0] == "false")) {
		vals = []string{"1=1", "true"}
	}
	norm := strings.ToLower(strings.ReplaceAll(stmt, " ", ""))
	for _, t := range vals {
		t = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(t), " ", ""))
		if t != "" && strings.Contains(norm, t) {
			return "WHERE 条件视为恒真（" + t + "）"
		}
	}
	return ""
}

func updateSetUsesAnd(stmt string) bool {
	up := strings.ToUpper(stmt)
	i := strings.Index(up, "SET")
	if i < 0 {
		return false
	}
	rest := stmt[i+3:]
	if loc := reWhere.FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	return regexp.MustCompile(`(?i)\bAND\b`).MatchString(rest)
}

func countCreateTableColumns(stmt string) int {
	i := strings.Index(stmt, "(")
	if i < 0 {
		return 0
	}
	depth, last := 0, i
	for j := i; j < len(stmt); j++ {
		switch stmt[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				body := stmt[last+1 : j]
				if strings.TrimSpace(body) == "" {
					return 0
				}
				return strings.Count(body, ",") + 1
			}
		}
	}
	return 0
}

func countInsertRows(stmt string) int {
	up := strings.ToUpper(stmt)
	i := strings.LastIndex(up, "VALUES")
	if i < 0 {
		return 0
	}
	return strings.Count(stmt[i:], "),(") + 1
}

func isMultiTableDML(kw, upper string) bool {
	if kw == "UPDATE" {
		return reUpdateFrom.MatchString(upper) || strings.Contains(upper, " JOIN ")
	}
	if kw == "DELETE" {
		return reDeleteJoin.MatchString(upper)
	}
	return false
}

func rulePrefix(r Rule) string {
	s := strings.TrimSpace(r.Value)
	if strings.HasPrefix(s, "{") {
		if p := ruleField(s, "prefix", ""); p != "" {
			return p
		}
	}
	switch strings.ToLower(s) {
	case "", "true", "false", "1", "0", "on", "off":
		return ""
	}
	return s
}

func isAlterAddColumn(upper string) bool {
	if !reAlterAdd.MatchString(upper) {
		return false
	}
	if reAddCheck.MatchString(upper) || reAddFK.MatchString(upper) || reAddNonColumn.MatchString(upper) {
		return false
	}
	return true
}

func createTableName(stmt string) string {
	m := reCreateTableName.FindStringSubmatch(stmt)
	if len(m) < 2 {
		return ""
	}
	return strings.Trim(m[1], "`\"")
}

func createDBName(stmt string) string {
	names := ParseCreateDBNames(stmt)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func isConstantDefault(stmt string) bool {
	return reConstDefault.MatchString(stmt)
}

func hasDisallowedType(stmt string, byName map[string]Rule) bool {
	r, ok := ruleOf(byName, "DDLColumnTypeDisallow")
	if !ok {
		return reFloatDouble.MatchString(strings.ToUpper(stmt))
	}
	types := ruleStringSlice(r.Value, "disallow_types", nil)
	if len(types) == 0 {
		types = splitCSV(r.Value)
	}
	if len(types) == 0 {
		types = []string{"float", "double", "double precision", "real"}
	}
	up := strings.ToUpper(stmt)
	for _, typ := range types {
		typ = strings.TrimSpace(typ)
		if typ == "" || typ == "true" || typ == "false" {
			continue
		}
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(typ) + `\b`).MatchString(up) {
			return true
		}
	}
	return false
}

func splitCSV(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func dropTableName(stmt string) string {
	m := reDropTableName.FindStringSubmatch(stmt)
	if len(m) < 2 {
		return ""
	}
	return strings.Trim(m[1], "`\"")
}

func ruleMsg(byName map[string]Rule, name, fallback string) string {
	if r, ok := ruleOf(byName, name); ok && strings.TrimSpace(r.Label) != "" {
		return r.Label
	}
	return fallback
}

func ruleOn(value string) bool {
	s := strings.TrimSpace(value)
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "{") {
		var o map[string]any
		if json.Unmarshal([]byte(s), &o) == nil {
			if v, ok := o["enabled"]; ok {
				return asBool(v)
			}
			if v, ok := o["unlimited"]; ok && asBool(v) {
				return false
			}
		}
		return true
	}
	switch strings.ToLower(s) {
	case "false", "0", "off", "no", "disabled":
		return false
	default:
		return true
	}
}

func ruleAllow(value string) bool {
	s := strings.TrimSpace(value)
	if strings.HasPrefix(s, "{") {
		var o map[string]any
		if json.Unmarshal([]byte(s), &o) == nil {
			if v, ok := o["allow"]; ok {
				return asBool(v)
			}
		}
	}
	return asBool(s)
}

func ruleUnlimited(byName map[string]Rule, name string) bool {
	r, ok := ruleOf(byName, name)
	if !ok {
		return false
	}
	s := strings.TrimSpace(r.Value)
	if !strings.HasPrefix(s, "{") {
		return false
	}
	var o map[string]any
	if json.Unmarshal([]byte(s), &o) != nil {
		return false
	}
	return asBool(o["unlimited"])
}

func ruleInt(byName map[string]Rule, def int, names ...string) (int, bool) {
	r, ok := ruleOf(byName, names...)
	if !ok {
		return def, false
	}
	s := strings.TrimSpace(r.Value)
	if s == "" {
		return def, true
	}
	if strings.HasPrefix(s, "{") {
		var o map[string]any
		if json.Unmarshal([]byte(s), &o) == nil {
			for _, k := range names {
				if n, ok := asInt(o[k]); ok {
					return n, true
				}
			}
			for _, k := range []string{"max_columns", "max_rows", "max_char", "max_varchar", "value", "max_len"} {
				if n, ok := asInt(o[k]); ok {
					return n, true
				}
			}
		}
		return def, true
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	return def, true
}

func ruleField(value, key, def string) string {
	s := strings.TrimSpace(value)
	if strings.HasPrefix(s, "{") {
		var o map[string]any
		if json.Unmarshal([]byte(s), &o) == nil {
			if v, ok := o[key]; ok {
				return strings.TrimSpace(asString(v))
			}
		}
		return def
	}
	if key == "mode" && s != "" && s != "true" && s != "false" {
		return s
	}
	return def
}

func ruleStringSlice(value, key string, def []string) []string {
	s := strings.TrimSpace(value)
	if strings.HasPrefix(s, "{") {
		var o map[string]any
		if json.Unmarshal([]byte(s), &o) == nil {
			if v, ok := o[key]; ok {
				return asStringSlice(v)
			}
		}
	}
	return def
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	default:
		return false
	}
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case json.Number:
		n, err := t.Int64()
		return int(n), err == nil
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		return n, err == nil
	default:
		return 0, false
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func asStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			if s := strings.TrimSpace(asString(it)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return []string{t}
	default:
		return nil
	}
}

var (
	reRevoke           = regexp.MustCompile(`(?:^|;)\s*REVOKE\b`)
	reGrant            = regexp.MustCompile(`(?:^|;)\s*GRANT\b`)
	reCreateDatabase   = regexp.MustCompile(`CREATE\s+DATABASE`)
	reDropDatabase     = regexp.MustCompile(`DROP\s+DATABASE`)
	reDropTable        = regexp.MustCompile(`DROP\s+TABLE`)
	reDropSchema       = regexp.MustCompile(`DROP\s+SCHEMA`)
	reTruncate         = regexp.MustCompile(`TRUNCATE\b`)
	reDropColumn       = regexp.MustCompile(`DROP\s+COLUMN`)
	reCreateTable      = regexp.MustCompile(`CREATE\s+TABLE`)
	reAlterAdd         = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+.+\s+ADD\s+`)
	reAddNonColumn     = regexp.MustCompile(`(?i)ADD\s+(?:CONSTRAINT|INDEX|UNIQUE|PRIMARY|FULLTEXT|SPATIAL|KEY)\b`)
	reCreateTableAS    = regexp.MustCompile(`(?i)CREATE\s+TABLE[\s\S]+\bAS\s+SELECT\b`)
	reSetNotNull       = regexp.MustCompile(`(?i)ALTER\s+COLUMN\s+\S+\s+SET\s+NOT\s+NULL`)
	reAddCheck         = regexp.MustCompile(`(?i)ADD\s+(?:CONSTRAINT\s+\S+\s+)?CHECK\s*\(`)
	reAddFK            = regexp.MustCompile(`(?i)ADD\s+(?:CONSTRAINT\s+\S+\s+)?FOREIGN\s+KEY`)
	reIndexDDL         = regexp.MustCompile(`(?i)((CREATE|DROP)\s+(UNIQUE\s+)?INDEX|ADD\s+CONSTRAINT\s+\S+\s+UNIQUE)`)
	reRename           = regexp.MustCompile(`(?i)\bRENAME\b`)
	reAlterType        = regexp.MustCompile(`(?i)(ALTER\s+COLUMN\s+\S+\s+TYPE|MODIFY\s+COLUMN)`)
	reCreateTableName  = regexp.MustCompile(`(?i)CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+(?:[\w."]+\.)?["\x60]?([A-Za-z0-9_]+)`)
	reConstDefault     = regexp.MustCompile(`(?i)DEFAULT\s+('([^']*)'|"([^"]*)"|-?\d+(?:\.\d+)?)`)
	reUpdateFrom       = regexp.MustCompile(`(?i)\bUPDATE\b[\s\S]+\bFROM\b`)
	reDeleteJoin       = regexp.MustCompile(`(?i)\bDELETE\b[\s\S]+\b(USING|JOIN)\b`)
	reLockTable        = regexp.MustCompile(`\bLOCK\s+TABLE\b`)
	reVacuumFull       = regexp.MustCompile(`\bVACUUM\s+FULL\b`)
	reCluster          = regexp.MustCompile(`(?:^|;)\s*CLUSTER\b`)
	reCommentMySQL     = regexp.MustCompile(`(?i)COMMENT\s+['"]`)
	reCommentPG        = regexp.MustCompile(`(?i)COMMENT\s+ON\s+(TABLE|COLUMN|CONSTRAINT)\s+.+?\s+IS\s*['"]`)
	reNotNull          = regexp.MustCompile(`(?i)NOT\s+NULL`)
	reDefault          = regexp.MustCompile(`(?i)DEFAULT\s+`)
	reFloatDouble      = regexp.MustCompile(`\b(FLOAT|DOUBLE)\b`)
	rePrimaryKey       = regexp.MustCompile(`PRIMARY\s+KEY`)
	reStarRocksKey     = regexp.MustCompile(`(?i)\b(UNIQUE|DUPLICATE|PRIMARY|AGGREGATE)\s+KEY\b`)
	reBlob             = regexp.MustCompile(`\b(BLOB|TINYBLOB|MEDIUMBLOB|LONGBLOB)\b`)
	reTimestamp        = regexp.MustCompile(`\bTIMESTAMP\b`)
	reVarcharLen       = regexp.MustCompile(`(?i)\b(?:character\s+varying|varchar)\s*\(\s*(\d+)\s*\)`)
	reCharLen          = regexp.MustCompile(`(?i)\bchar(?:acter)?\s*\(\s*(\d+)\s*\)`)
	reInsertCols       = regexp.MustCompile(`(?is)^\s*INSERT\s+(?:IGNORE\s+)?(?:INTO\s+)?\S+\s*\(`)
	reInsertSelectStar = regexp.MustCompile(`INSERT\s+[\s\S]+SELECT\s+\*`)
	reSelectStar       = regexp.MustCompile(`SELECT\s+\*`)
	reOrderByRand      = regexp.MustCompile(`ORDER\s+BY\s+(RANDOM|RAND)\s*\(`)
	reHasSelect        = regexp.MustCompile(`\bSELECT\b`)
	reDropTableName    = regexp.MustCompile(`(?i)DROP\s+TABLE(?:\s+IF\s+EXISTS)?\s+(?:[\w."]+\.)?["\x60]?([A-Za-z0-9_]+)`)
)
