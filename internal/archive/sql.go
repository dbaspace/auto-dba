package archive

import (
	"fmt"
	"regexp"
	"strings"

	"auto-dba/internal/store"
)

const (
	mysqlBatchSize = 10000
	pgCTEBatchSize = 5000
	maxRounds      = 200000
	shardThreshold = 100000
)

var (
	identRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	dangerRe = regexp.MustCompile(`(?i)\b(drop|truncate|insert|update|alter|create|grant|revoke|copy|call|execute)\b`)
	limitRe  = regexp.MustCompile(`(?i)\blimit\b`)
)

func NormalizeStrategy(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "direct_delete", "delete_only", "direct-delete":
		return "delete_only"
	case "backup_target", "backup":
		return "backup_target"
	case "archive_oss", "archive", "archive_only":
		return "archive_oss"
	case "custom_script", "custom-script", "script":
		return "custom_script"
	default:
		return strings.TrimSpace(s)
	}
}

func quoteIdent(dbType, ident string) string {
	ident = strings.TrimSpace(ident)
	if store.NormalizeDBType(dbType) == "postgres" {
		return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}

func validateIdent(name, field string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%s 不能为空", field)
	}
	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		for _, p := range parts {
			if !identRe.MatchString(strings.Trim(p, `"'`+"`")) {
				return fmt.Errorf("%s 非法标识符: %s", field, name)
			}
		}
		return nil
	}
	if !identRe.MatchString(name) {
		return fmt.Errorf("%s 非法标识符: %s", field, name)
	}
	return nil
}

func normalizeCondition(cond string) string {
	cond = strings.TrimSpace(cond)
	cond = strings.TrimRight(cond, "; \t")
	return strings.TrimSpace(cond)
}

func ValidateDeleteCondition(cond string) error {
	cond = normalizeCondition(cond)
	if cond == "" {
		return fmt.Errorf("删除条件不能为空")
	}
	if strings.Contains(cond, ";") {
		return fmt.Errorf("删除条件不能包含分号")
	}
	if dangerRe.MatchString(cond) {
		return fmt.Errorf("删除条件包含不允许的关键字")
	}
	if limitRe.MatchString(cond) {
		return fmt.Errorf("删除条件不能自带 LIMIT，由执行器统一追加")
	}
	if strings.HasPrefix(strings.ToLower(cond), "where ") {
		return fmt.Errorf("删除条件请勿包含 WHERE 关键字，只填写条件表达式")
	}
	return nil
}

func qualifyTable(dbType, table string) string {
	table = strings.TrimSpace(table)
	if strings.Contains(table, ".") {
		parts := strings.Split(table, ".")
		for i, p := range parts {
			parts[i] = quoteIdent(dbType, strings.Trim(p, `"'`+"`"))
		}
		return strings.Join(parts, ".")
	}
	return quoteIdent(dbType, table)
}

func BuildMySQLDeleteSQL(table, condition string) (string, error) {
	if err := validateIdent(table, "source_table"); err != nil {
		return "", err
	}
	if err := ValidateDeleteCondition(condition); err != nil {
		return "", err
	}
	return fmt.Sprintf("DELETE FROM %s WHERE %s LIMIT %d", qualifyTable("mysql", table), normalizeCondition(condition), mysqlBatchSize), nil
}

func BuildPGCTESQL(table, condition string, pkCols []string) (string, error) {
	if err := validateIdent(table, "source_table"); err != nil {
		return "", err
	}
	if err := ValidateDeleteCondition(condition); err != nil {
		return "", err
	}
	qt := qualifyTable("postgres", table)
	if len(pkCols) == 0 {
		return fmt.Sprintf(
			`WITH d AS (SELECT ctid FROM %s WHERE %s LIMIT %d)
DELETE FROM %s WHERE ctid IN (SELECT ctid FROM d)`,
			qt, normalizeCondition(condition), pgCTEBatchSize, qt,
		), nil
	}
	qPKs := make([]string, 0, len(pkCols))
	for _, c := range pkCols {
		qPKs = append(qPKs, quoteIdent("postgres", c))
	}
	pkList := strings.Join(qPKs, ", ")
	pkTuple := pkList
	if len(pkCols) > 1 {
		pkTuple = "(" + pkList + ")"
	}
	return fmt.Sprintf(
		`WITH cte1 AS (
  SELECT %s FROM %s WHERE %s LIMIT %d
),
cte2 AS (
  DELETE FROM %s WHERE %s IN (SELECT %s FROM cte1)
)
SELECT * FROM cte1`,
		pkList, qt, normalizeCondition(condition), pgCTEBatchSize,
		qt, pkTuple, pkList,
	), nil
}

func ResolvePGMode(reqMode string, hasCreateTime bool, estimatedRows int64) (string, error) {
	switch strings.ToLower(strings.TrimSpace(reqMode)) {
	case "cte":
		return "cte", nil
	case "shard":
		if !hasCreateTime {
			return "", fmt.Errorf("分片删除需要表含 create_time 字段，请改用循环删除(cte)或自动")
		}
		return "shard", nil
	case "", "auto":
		if hasCreateTime && estimatedRows >= shardThreshold {
			return "shard", nil
		}
		return "cte", nil
	default:
		return "", fmt.Errorf("非法 pg_delete_mode: %s，仅支持 cte / shard / auto", reqMode)
	}
}

func BuildSelectSQL(dbType, table, condition string, limit int) (string, error) {
	if err := validateIdent(table, "source_table"); err != nil {
		return "", err
	}
	if err := ValidateDeleteCondition(condition); err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = mysqlBatchSize
	}
	return fmt.Sprintf("SELECT * FROM %s WHERE %s LIMIT %d", qualifyTable(dbType, table), normalizeCondition(condition), limit), nil
}
