package dbmig

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

func decrypt(secretKey string, inst *store.Instance) (string, error) {
	if inst == nil {
		return "", fmt.Errorf("实例不存在")
	}
	if strings.TrimSpace(inst.Password) == "" {
		return "", nil
	}
	return secret.Decrypt(secretKey, inst.Password)
}

func instHost(inst *store.Instance) string {
	h := strings.TrimSpace(inst.Host)
	if h == "" {
		h = strings.TrimSpace(inst.DomainName)
	}
	return h
}

func Open(ctx context.Context, engine, host string, port int, user, password, dbName string) (*sql.DB, error) {
	if host == "" || port <= 0 {
		return nil, fmt.Errorf("未配置连接主机或端口")
	}
	if strings.TrimSpace(user) == "" {
		return nil, fmt.Errorf("未配置账号")
	}
	if dbName == "" {
		dbName = defaultDB(engine)
	}
	var (
		db  *sql.DB
		err error
	)
	switch store.NormalizeDBType(engine) {
	case "mysql", "tidb", "oceanbase", "starrocks":
		db, err = openMySQL(host, port, user, password, dbName)
	case "postgres":
		db, err = openPostgres(host, port, user, password, dbName)
	default:
		return nil, fmt.Errorf("仅支持 MySQL / PostgreSQL 迁移，当前类型 %s", engine)
	}
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func OpenInstance(ctx context.Context, inst *store.Instance, secretKey, dbName string) (*sql.DB, error) {
	plain, err := decrypt(secretKey, inst)
	if err != nil {
		return nil, fmt.Errorf("解密实例密码失败: %w", err)
	}
	return Open(ctx, inst.DBType, instHost(inst), inst.Port, inst.Username, plain, dbName)
}

func defaultDB(engine string) string {
	if store.NormalizeDBType(engine) == "postgres" {
		return "postgres"
	}
	return ""
}

func openMySQL(host string, port int, user, password, dbName string) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = dbName
	cfg.Timeout = 12 * time.Second
	cfg.ReadTimeout = 10 * time.Minute
	cfg.WriteTimeout = 10 * time.Minute
	cfg.Params = map[string]string{"charset": "utf8mb4", "parseTime": "true"}
	cfg.MultiStatements = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(2 * time.Hour)
	db.SetMaxOpenConns(4)
	return db, nil
}

func openPostgres(host string, port int, user, password, dbName string) (*sql.DB, error) {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + dbName,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", "12")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(2 * time.Hour)
	db.SetMaxOpenConns(4)
	return db, nil
}

func quoteIdent(engine, name string) string {
	if store.NormalizeDBType(engine) == "postgres" {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func splitTable(engine, raw string) (schema, table string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if i := strings.LastIndex(raw, "."); i > 0 {
		return raw[:i], raw[i+1:]
	}
	if store.NormalizeDBType(engine) == "postgres" {
		return "public", raw
	}
	return "", raw
}

func tableRef(engine, schema, table string) string {
	if store.NormalizeDBType(engine) == "postgres" {
		if schema == "" {
			schema = "public"
		}
		return quoteIdent(engine, schema) + "." + quoteIdent(engine, table)
	}
	return quoteIdent(engine, table)
}
