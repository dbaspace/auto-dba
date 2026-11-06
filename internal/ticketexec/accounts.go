package ticketexec

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	sqlListPostgresRoles = `SELECT rolname FROM pg_roles WHERE rolname !~ '^pg_' ORDER BY 1`
	sqlListMySQLUsers    = `SELECT DISTINCT User FROM mysql.user WHERE User <> '' ORDER BY User`
)

// ListInstanceAccounts 列出实例上已有、可授权的账号。PostgreSQL 对齐 \du，不含 pg_ 系统角色。
func ListInstanceAccounts(ctx context.Context, t Target) ([]string, error) {
	switch EngineOf(t.DBType) {
	case "postgres":
		return listPostgresRoles(ctx, t)
	case "starrocks":
		return listStarRocksUsers(ctx, t)
	case "mysql":
		return listMySQLUsers(ctx, t)
	default:
		return nil, fmt.Errorf("不支持的数据库类型")
	}
}

func listPostgresRoles(ctx context.Context, t Target) ([]string, error) {
	admin := t
	admin.ConnectDB = AdminConnectDB(t.DBType)
	db, err := openPostgres(admin, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, sqlListPostgresRoles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccountNames(rows)
}

func listMySQLUsers(ctx context.Context, t Target) ([]string, error) {
	admin := t
	admin.ConnectDB = ""
	admin.DBName = ""
	db, err := openMySQL(admin)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, sqlListMySQLUsers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccountNames(rows)
}

func listStarRocksUsers(ctx context.Context, t Target) ([]string, error) {
	admin := t
	admin.ConnectDB = ""
	admin.DBName = ""
	db, err := openMySQL(admin)
	if err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SHOW USERS")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var idents []string
	for rows.Next() {
		var ident string
		if err := rows.Scan(&ident); err != nil {
			return nil, err
		}
		idents = append(idents, ident)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return uniqueStarRocksUsers(idents), nil
}

func uniqueStarRocksUsers(idents []string) []string {
	var names []string
	for _, ident := range idents {
		user, _ := parseStarRocksUserIdentity(ident)
		names = append(names, user)
	}
	return uniqueAccountNames(names)
}

func scanAccountNames(rows *sql.Rows) ([]string, error) {
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return uniqueAccountNames(names), nil
}

func uniqueAccountNames(names []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if out == nil {
		out = []string{}
	}
	return out
}
