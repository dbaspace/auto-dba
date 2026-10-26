package ticketexec

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// likePrefixPattern 把分库前缀转成 LIKE 字面匹配，并在末尾加 %。
// 前缀里的 _、%、\ 必须转义，否则 _ 会当成单字符通配。
func likePrefixPattern(prefix string) string {
	var b strings.Builder
	b.Grow(len(prefix) + 8)
	for _, r := range prefix {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('%')
	return b.String()
}

// ListPostgresDatabasesByPrefix 连管理库 postgres，按前缀列出库名（尚未按数字后缀过滤）。
func ListPostgresDatabasesByPrefix(ctx context.Context, t Target, prefix string) ([]string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, fmt.Errorf("分库前缀不能为空")
	}
	admin := t
	admin.ConnectDB = AdminConnectDB("postgres")
	db, err := openPostgres(admin, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT datname FROM pg_database WHERE datname LIKE $1 ESCAPE '\' ORDER BY datname`,
		likePrefixPattern(prefix))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}
