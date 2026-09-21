package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

func ListPGTables(ctx context.Context, db *sql.DB, schema string) ([]string, error) {
	schema = defaultStr(schema, "public")
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=$1 AND table_type='BASE TABLE' ORDER BY table_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if n != "" && !strings.EqualFold(n, DefaultDebeziumSignalTable) {
			out = append(out, n)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}

func GlobToRegex(p string) string {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteByte('$')
	return b.String()
}

func ShardGlobToMatchRegex(pattern string) string {
	p := strings.TrimSpace(pattern)
	if t := strings.TrimRight(p, "*"); t != p && !strings.ContainsAny(t, "*?") {
		stem := strings.TrimRight(t, "_")
		if stem != "" {
			esc := regexp.QuoteMeta(stem)
			return "^" + esc + "(_[0-9]+|[0-9]+)$"
		}
	}
	return GlobToRegex(p)
}

func MatchShardTables(ctx context.Context, db *sql.DB, schema, pattern, patternType string) ([]string, error) {
	all, err := ListPGTables(ctx, db, schema)
	if err != nil {
		return nil, err
	}
	return FilterTablesByPattern(all, pattern, patternType)
}

func FilterTablesByPattern(all []string, pattern, patternType string) ([]string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	var (
		re  *regexp.Regexp
		err error
	)
	switch strings.ToLower(strings.TrimSpace(patternType)) {
	case "regex":
		re, err = regexp.Compile(pattern)
	default:
		re, err = regexp.Compile(ShardGlobToMatchRegex(pattern))
	}
	if err != nil {
		return nil, fmt.Errorf("compile pattern: %w", err)
	}
	var out []string
	for _, t := range all {
		if re.MatchString(t) {
			out = append(out, t)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}
