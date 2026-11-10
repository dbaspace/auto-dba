package dbprobe

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	patShowDeniedHint    = "PAT 查询不允许该 SHOW 语句"
	patShowStarRocksHint = "仅 StarRocks 可以执行该 SHOW"
	patSecretReadHint    = "PAT 查询不允许读取账号、口令或服务器文件"
	patMetaTableHint     = "PAT 查询不允许读取平台登记信息"
)

var (
	rePATSecretRead = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(pg_authid|pg_shadow|pg_read_file|pg_read_binary_file|lo_import|lo_export|dblink_exec|dblink|load_file)([^a-z0-9_]|$)`)
	rePATMySQLUser  = regexp.MustCompile(`(?i)(^|[^a-z0-9_])mysql\s*\.\s*user([^a-z0-9_]|$)`)
	rePATMetaTable  = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(tbl_users|tbl_instances|tbl_db_accounts|tbl_client_pats|tbl_global_args|tbl_nacos_admin|tbl_oss_settings)([^a-z0-9_]|$)`)
	rePATShowWrite  = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(into|outfile|dumpfile)([^a-z0-9_]|$)`)
)

type patShowClass int

const (
	patShowDeny patShowClass = iota
	patShowAny
	patShowStarRocks
)

// PATQueryBlocked 返回第一条 PAT 不能执行的语句。浏览器查询不调用。
func PATQueryBlocked(stmts []string) (idx int, msg string, blocked bool) {
	for i, stmt := range stmts {
		if reason := patQueryBlockReason(stmt); reason != "" {
			if len(stmts) > 1 {
				reason = fmt.Sprintf("第 %d 条 SQL：%s", i+1, reason)
			}
			return i, reason, true
		}
	}
	return 0, "", false
}

// PATStarRocksShowOnly 在已经知道目标库引擎后调用。StarRocks 专有 SHOW 落到其它引擎时拒绝。
func PATStarRocksShowOnly(stmts []string, dbType string) (idx int, msg string, blocked bool) {
	if normType(dbType) == "starrocks" {
		return 0, "", false
	}
	for i, stmt := range stmts {
		if classifyPATShow(stmt) != patShowStarRocks {
			continue
		}
		reason := patShowStarRocksHint
		if len(stmts) > 1 {
			reason = fmt.Sprintf("第 %d 条 SQL：%s", i+1, reason)
		}
		return i, reason, true
	}
	return 0, "", false
}

func patQueryBlockReason(stmt string) string {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(stmt), ";"))
	if body == "" {
		return ""
	}
	scanned := stripIdentQuotes(maskSQLCommentsAndStrings(body))
	if rePATMetaTable.MatchString(scanned) {
		return patMetaTableHint
	}
	if rePATSecretRead.MatchString(scanned) || rePATMySQLUser.MatchString(scanned) {
		return patSecretReadHint
	}
	switch classifyPATShow(body) {
	case patShowDeny:
		if firstSQLKeyword(body) == "SHOW" {
			return patShowDeniedHint
		}
	case patShowAny, patShowStarRocks:
		if rePATShowWrite.MatchString(scanned) {
			return patShowDeniedHint
		}
	}
	return ""
}

func isAdminShow(sql string) bool {
	fields := strings.Fields(stripIdentQuotes(maskSQLCommentsAndStrings(sql)))
	return len(fields) >= 2 && strings.EqualFold(fields[0], "ADMIN") && strings.EqualFold(fields[1], "SHOW")
}

func classifyPATShow(stmt string) patShowClass {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(stmt), ";"))
	scanned := stripIdentQuotes(maskSQLCommentsAndStrings(body))
	fields := strings.Fields(scanned)
	if isAdminShow(body) {
		return patShowStarRocks
	}
	if len(fields) < 2 || !strings.EqualFold(fields[0], "SHOW") {
		return patShowDeny
	}
	i := 1
	if strings.EqualFold(fields[i], "FULL") {
		i++
		if i >= len(fields) {
			return patShowDeny
		}
	}
	rest := fields[i:]
	if hasShowPrefix(rest, "CREATE", "USER") ||
		hasShowPrefix(rest, "CREATE", "STORAGE", "VOLUME") ||
		hasShowPrefix(rest, "GRANTS") ||
		hasShowPrefix(rest, "USERS") ||
		hasShowPrefix(rest, "ROLES") ||
		hasShowPrefix(rest, "PROPERTY") ||
		hasShowPrefix(rest, "AUTHENTICATION") ||
		hasShowPrefix(rest, "REPOSITORIES") ||
		hasShowPrefix(rest, "STORAGE", "VOLUMES") ||
		hasShowPrefix(rest, "STORAGE", "VOLUME") ||
		hasShowPrefix(rest, "WHITELIST") ||
		hasShowPrefix(rest, "SQLBLACKLIST") {
		return patShowDeny
	}
	if hasShowPrefix(rest, "TABLES") ||
		hasShowPrefix(rest, "DATABASES") ||
		hasShowPrefix(rest, "SCHEMAS") ||
		hasShowPrefix(rest, "COLUMNS") ||
		hasShowPrefix(rest, "FIELDS") ||
		hasShowPrefix(rest, "INDEX") ||
		hasShowPrefix(rest, "INDEXES") ||
		hasShowPrefix(rest, "KEYS") ||
		hasShowPrefix(rest, "CREATE", "TABLE") ||
		hasShowPrefix(rest, "CREATE", "VIEW") {
		return patShowAny
	}
	return patShowStarRocks
}

func hasShowPrefix(fields []string, prefix ...string) bool {
	if len(fields) < len(prefix) {
		return false
	}
	for i, word := range prefix {
		if !strings.EqualFold(fields[i], word) {
			return false
		}
	}
	return true
}

func stripIdentQuotes(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '`' || r == '"' {
			return ' '
		}
		return r
	}, s)
}

// maskSQLCommentsAndStrings 去掉注释、单引号字符串和 dollar-quote，保留双引号与反引号里的标识符。
func maskSQLCommentsAndStrings(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "--") {
			if nl := strings.IndexByte(s[i:], '\n'); nl >= 0 {
				i += nl + 1
			} else {
				i = len(s)
			}
			b.WriteByte(' ')
			continue
		}
		if strings.HasPrefix(s[i:], "/*") {
			if end := strings.Index(s[i+2:], "*/"); end >= 0 {
				i += 2 + end + 2
			} else {
				i = len(s)
			}
			b.WriteByte(' ')
			continue
		}
		if s[i] == '\'' {
			i++
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			b.WriteByte(' ')
			continue
		}
		if s[i] == '$' {
			j := i + 1
			for j < len(s) && (s[j] == '_' || unicode.IsLetter(rune(s[j])) || unicode.IsDigit(rune(s[j]))) {
				j++
			}
			if j < len(s) && s[j] == '$' {
				tag := s[i : j+1]
				i = j + 1
				if k := strings.Index(s[i:], tag); k >= 0 {
					i += k + len(tag)
				} else {
					i = len(s)
				}
				b.WriteByte(' ')
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
