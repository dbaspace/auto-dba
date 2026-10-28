package dbprobe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 与老项目对齐的代码默认值；运行时以全局参数为准。
const (
	DefaultQueryLimit = 1000
	MaxQueryLimit     = 10000
)

var reTailLimitNum = regexp.MustCompile(`(?i)\s+LIMIT\s+(\d+)\s*;?\s*$`)

// EnsureQueryLimitResult 改写后的 SQL 与生效行数上限。
type EnsureQueryLimitResult struct {
	SQL          string // 改写后（limit 小写）
	AppliedLimit int    // 本语句实际 limit
	MaxLimit     int    // 硬顶（文案「最大查询」）
	DefaultLimit int
}

const exportLimitSanityMax = 1000000

// EnsureQueryLimit 对 SELECT/WITH 注入或钳制 limit（统一小写关键字）。
// SHOW/EXPLAIN/DESC 不改写，AppliedLimit 仍返回 maxN 供扫描上限使用。
func EnsureQueryLimit(raw string, defaultN, maxN int) EnsureQueryLimitResult {
	if defaultN <= 0 {
		defaultN = DefaultQueryLimit
	}
	if maxN <= 0 {
		maxN = MaxQueryLimit
	}
	return ensureSelectLimit(raw, defaultN, maxN, 100000)
}

// EnsureExportLimit 同 EnsureQueryLimit，供 DQL 导出；硬顶可达 100 万。
func EnsureExportLimit(raw string, defaultN, maxN int) EnsureQueryLimitResult {
	if defaultN <= 0 {
		defaultN = DefaultExportLimit
	}
	if maxN <= 0 {
		maxN = MaxExportLimit
	}
	return ensureSelectLimit(raw, defaultN, maxN, exportLimitSanityMax)
}

// ParseSQLTailLimit 解析语句尾部 LIMIT n（LIMIT a,b 取 b）；无则 ok=false。
func ParseSQLTailLimit(raw string) (n int, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	body := strings.TrimSpace(strings.TrimSuffix(s, ";"))
	m := reTailLimitNum.FindStringSubmatch(body)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// RequireExportSQLLimit 导出 SQL 必须自带 LIMIT；返回钳制后的扫描上限（代码硬顶 100 万）。
func RequireExportSQLLimit(raw string) (limit int, err error) {
	n, ok := ParseSQLTailLimit(raw)
	if !ok {
		return 0, fmt.Errorf("导出 SQL 必须包含 LIMIT")
	}
	if n > exportLimitSanityMax {
		n = exportLimitSanityMax
	}
	return n, nil
}

func ensureSelectLimit(raw string, defaultN, maxN, sanityMax int) EnsureQueryLimitResult {
	if sanityMax <= 0 {
		sanityMax = 100000
	}
	if maxN > sanityMax {
		maxN = sanityMax
	}
	if defaultN > maxN {
		defaultN = maxN
	}
	out := EnsureQueryLimitResult{DefaultLimit: defaultN, MaxLimit: maxN, AppliedLimit: defaultN}
	s := strings.TrimSpace(raw)
	if s == "" {
		out.SQL = s
		return out
	}
	semi := strings.HasSuffix(s, ";")
	body := strings.TrimSpace(strings.TrimSuffix(s, ";"))
	kw := firstSQLKeyword(body)
	switch kw {
	case "SELECT", "WITH":
	default:
		out.SQL = s
		out.AppliedLimit = maxN
		return out
	}
	if m := reTailLimitNum.FindStringSubmatch(body); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n <= 0 {
			n = defaultN
		}
		if n > maxN {
			n = maxN
		}
		body = strings.TrimSpace(reTailLimitNum.ReplaceAllString(body, ""))
		body = fmt.Sprintf("%s limit %d", body, n)
		out.AppliedLimit = n
	} else {
		body = fmt.Sprintf("%s limit %d", body, defaultN)
		out.AppliedLimit = defaultN
	}
	if semi {
		body += ";"
	}
	out.SQL = body
	return out
}

// SplitReadOnlyStatements 拆分只读多语句，最多 maxN 条（≤0 视为 1）。
// 任一条非只读则返回错误。
func SplitReadOnlyStatements(raw string, maxN int) ([]string, error) {
	if maxN <= 0 {
		maxN = 1
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("SQL 不能为空")
	}
	parts := splitSQLStatements(s)
	if len(parts) == 0 {
		return nil, fmt.Errorf("SQL 不能为空")
	}
	if len(parts) > maxN {
		return nil, fmt.Errorf("单次最多执行 %d 条 SQL", maxN)
	}
	for _, p := range parts {
		if err := IsReadOnlySQL(p); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

func splitSQLStatements(s string) []string {
	var out []string
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			b.WriteByte(c)
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			b.WriteByte(c)
			continue
		}
		if c == ';' && !inSingle && !inDouble {
			part := strings.TrimSpace(b.String())
			b.Reset()
			if part != "" {
				out = append(out, part)
			}
			continue
		}
		b.WriteByte(c)
	}
	if part := strings.TrimSpace(b.String()); part != "" {
		out = append(out, part)
	}
	return out
}
