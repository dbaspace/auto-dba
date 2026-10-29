package ticketexec

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	sqlLookupPGDatabaseOwner = `
SELECT r.rolname AS owner_name
FROM pg_database d
JOIN pg_roles r ON d.datdba = r.oid
WHERE d.datname = $1`
	sqlPostgresUserExists = `SELECT usename FROM pg_user WHERE usename = $1`
)

// LookupPostgresDatabaseOwner 在维护库会话按 datname 查库拥有者（pg_database.datdba）。
func LookupPostgresDatabaseOwner(ctx context.Context, t Target, dbName string) (string, error) {
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return "", fmt.Errorf("库名不能为空")
	}
	admin := t
	admin.ConnectDB = AdminConnectDB("postgres")
	db, err := openPostgres(admin, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("连接失败: %w", err)
	}
	defer db.Close()
	var owner string
	err = db.QueryRowContext(ctx, sqlLookupPGDatabaseOwner, dbName).Scan(&owner)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("库 %s 不存在或无法解析 owner", dbName)
	}
	if err != nil {
		return "", err
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "", fmt.Errorf("库 %s 的 owner 为空", dbName)
	}
	return owner, nil
}

// PostgresUserExists 查询 pg_user 是否已有该登录名。
func PostgresUserExists(ctx context.Context, t Target, username string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, fmt.Errorf("用户名不能为空")
	}
	admin := t
	admin.ConnectDB = AdminConnectDB("postgres")
	db, err := openPostgres(admin, 30*time.Second)
	if err != nil {
		return false, fmt.Errorf("连接失败: %w", err)
	}
	defer db.Close()
	var name string
	err = db.QueryRowContext(ctx, sqlPostgresUserExists, username).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(name) != "", nil
}
