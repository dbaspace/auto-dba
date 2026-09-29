package ticketexec

import (
	"regexp"
	"strings"
	"time"

	"auto-dba/internal/sqlcheck"
)

// DefaultLongStmtTimeout PG 长耗时语句（CREATE INDEX CONCURRENTLY 等）单条执行超时默认值。
const DefaultLongStmtTimeout = 6 * time.Hour

var longRunningPGPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)^create\s+(unique\s+)?index\s+concurrently\b`),
	regexp.MustCompile(`(?is)^reindex\b.*\bconcurrently\b`),
	regexp.MustCompile(`(?is)^drop\s+index\s+concurrently\b`),
	regexp.MustCompile(`(?is)^refresh\s+materialized\s+view\s+concurrently\b`),
	regexp.MustCompile(`(?is)^alter\s+table\b.*\bvalidate\s+constraint\b`),
}

const pgIdent = `(?:"(?:[^"]|"")+"|[A-Za-z_][A-Za-z0-9_$]*)`

var cicIndexNameRe = regexp.MustCompile(`(?is)^create\s+(?:unique\s+)?index\s+concurrently\s+(?:if\s+not\s+exists\s+)?(` +
	pgIdent + `(?:\s*\.\s*` + pgIdent + `)?)\s+on\b`)

// stripLeadingComments 去掉语句开头的空白、-- 行注释与 /* */ 块注释。
func stripLeadingComments(s string) string {
	for {
		s = strings.TrimSpace(s)
		switch {
		case strings.HasPrefix(s, "--"):
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return ""
			}
			s = s[i+1:]
		case strings.HasPrefix(s, "/*"):
			i := strings.Index(s, "*/")
			if i < 0 {
				return ""
			}
			s = s[i+2:]
		default:
			return s
		}
	}
}

// IsLongRunningPGStatement 是否为 PG 长耗时语句（并发建/删索引、并发刷新物化视图、校验约束等）。
func IsLongRunningPGStatement(stmt string) bool {
	s := stripLeadingComments(stmt)
	for _, re := range longRunningPGPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func HasLongRunningPGStatement(stmts []string) bool {
	for _, s := range stmts {
		if IsLongRunningPGStatement(s) {
			return true
		}
	}
	return false
}

// parseCICIndexName 取 CREATE INDEX CONCURRENTLY 的索引名（保留 schema 前缀与引号原样）；未写索引名时返回空。
func parseCICIndexName(stmt string) string {
	m := cicIndexNameRe.FindStringSubmatch(stripLeadingComments(stmt))
	if m == nil {
		return ""
	}
	name := strings.TrimSpace(m[1])
	if strings.EqualFold(name, "on") {
		return ""
	}
	return name
}

func effectiveLongTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultLongStmtTimeout
	}
	return d
}

// runTimeout 单目标整体超时：PG 且含长耗时语句时在 ddlTimeout 基础上加长语句超时。
func runTimeout(engine string, stmts []string, long time.Duration) time.Duration {
	if engine == "postgres" && HasLongRunningPGStatement(stmts) {
		return ddlTimeout + effectiveLongTimeout(long)
	}
	return ddlTimeout
}

// stmtTimeout 单条语句超时；long<=0 表示不区分语句（MySQL/StarRocks），返回 0 由整体超时约束。
func stmtTimeout(stmt string, long time.Duration) time.Duration {
	if long <= 0 {
		return 0
	}
	if IsLongRunningPGStatement(stmt) {
		return long
	}
	return ddlTimeout
}

// AsyncMaxWait 后台执行总墙钟：含长耗时语句时按目标数追加长语句超时。
func AsyncMaxWait(sqlContent string, targets int, long time.Duration) time.Duration {
	if targets < 1 {
		targets = 1
	}
	if !HasLongRunningPGStatement(sqlcheck.SplitSQL(sqlContent)) {
		return AsyncAutoExecMaxWait
	}
	return AsyncAutoExecMaxWait + time.Duration(targets)*effectiveLongTimeout(long)
}
