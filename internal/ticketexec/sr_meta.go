package ticketexec

import (
	"context"
	"fmt"
	"strings"
)

// StarRocksUserExists 通过 SHOW USERS 判断 'user'@'%' 是否已存在。
func StarRocksUserExists(ctx context.Context, t Target, username string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, fmt.Errorf("用户名不能为空")
	}
	admin := t
	admin.ConnectDB = ""
	admin.DBName = ""
	db, err := openMySQL(admin)
	if err != nil {
		return false, fmt.Errorf("连接失败: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SHOW USERS")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var ident string
		if err := rows.Scan(&ident); err != nil {
			return false, err
		}
		user, host := parseStarRocksUserIdentity(ident)
		if user == username && host == "%" {
			return true, nil
		}
	}
	return false, rows.Err()
}

// parseStarRocksUserIdentity 解析 SHOW USERS 返回的 'user'@'host'。
func parseStarRocksUserIdentity(ident string) (user, host string) {
	ident = strings.TrimSpace(ident)
	at := strings.LastIndex(ident, "@")
	if at < 0 {
		return strings.Trim(ident, "'`\""), "%"
	}
	user = strings.Trim(ident[:at], "'`\"")
	host = strings.Trim(ident[at+1:], "'`\"")
	return strings.ReplaceAll(user, "''", "'"), host
}
