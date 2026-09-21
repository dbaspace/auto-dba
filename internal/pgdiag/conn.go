package pgdiag

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/store"
)

const (
	defaultDB      = "postgres"
	queryTimeout   = 20 * time.Second
	connectTimeout = 8
)

func IsPostgres(dbType string) bool {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "postgres", "postgresql", "pg":
		return true
	default:
		return false
	}
}

func InstanceHost(inst *store.Instance) string {
	if inst == nil {
		return ""
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	return host
}

func ListPostgresInstances(db *store.DB) ([]store.Instance, error) {
	all, err := db.ListInstances("", "")
	if err != nil {
		return nil, err
	}
	out := make([]store.Instance, 0)
	for _, it := range all {
		if IsPostgres(it.DBType) {
			out = append(out, it)
		}
	}
	return out, nil
}

func Open(inst *store.Instance, password string) (*sql.DB, error) {
	if inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	host := InstanceHost(inst)
	if host == "" || inst.Port <= 0 {
		return nil, fmt.Errorf("实例未配置连接主机或端口")
	}
	user := strings.TrimSpace(inst.Username)
	if user == "" {
		return nil, fmt.Errorf("实例未配置账号")
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, inst.Port),
		Path:   "/" + defaultDB,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", fmt.Sprintf("%d", connectTimeout))
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(30 * time.Second)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(0)
	return db, nil
}

func pingQuery(ctx context.Context, inst *store.Instance, password string) (*sql.DB, context.Context, context.CancelFunc, error) {
	db, err := Open(inst, password)
	if err != nil {
		return nil, nil, nil, err
	}
	qctx, cancel := context.WithTimeout(ctx, queryTimeout)
	if err := db.PingContext(qctx); err != nil {
		cancel()
		_ = db.Close()
		return nil, nil, nil, fmt.Errorf("连接 %s:%d 失败: %w", InstanceHost(inst), inst.Port, err)
	}
	return db, qctx, cancel, nil
}
