package dbprobe

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"auto-dba/internal/store"
)

var systemDBs = map[string]struct{}{
	"information_schema": {},
	"performance_schema": {},
	"sys":                {},
	"mysql":              {},
	"template0":          {},
	"template1":          {},
	"postgres":           {},
	"defaultdb":          {},
	"_statistics_":       {},
	"system":             {},
	"master":             {},
	"model":              {},
	"msdb":               {},
	"tempdb":             {},
	"admin":              {},
	"local":              {},
	"config":             {},
	"system_schema":      {},
	"system_auth":        {},
	"system_distributed": {},
	"system_traces":      {},
}

type Item struct {
	Name   string `json:"name"`
	System bool   `json:"system"`
}

func IsSystem(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, ".") {
		return true
	}
	_, ok := systemDBs[n]
	return ok
}

func List(ctx context.Context, inst *store.Instance, password string) ([]Item, error) {
	if inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" || inst.Port <= 0 {
		return nil, fmt.Errorf("实例未配置连接主机或端口")
	}
	user := strings.TrimSpace(inst.Username)
	if user == "" {
		return nil, fmt.Errorf("实例未配置账号")
	}
	dbType := strings.ToLower(strings.TrimSpace(inst.DBType))
	var names []string
	var err error
	switch family(dbType) {
	case "postgres":
		names, err = listPostgres(ctx, host, inst.Port, user, password, pgConnectDB(dbType))
	case "mysql":
		names, err = listMySQL(ctx, host, inst.Port, user, password)
	case "clickhouse":
		names, err = listClickHouse(ctx, host, inst.Port, user, password)
	case "sqlserver":
		names, err = listSQLServer(ctx, host, inst.Port, user, password)
	case "oracle":
		names, err = listOracle(ctx, host, inst.Port, user, password)
	case "mongodb":
		names, err = listMongo(ctx, host, inst.Port, user, password)
	case "elasticsearch":
		names, err = listElastic(ctx, host, inst.Port, user, password)
	case "cassandra":
		names, err = listCassandra(ctx, host, inst.Port, user, password)
	case "kafka":
		return nil, fmt.Errorf("Kafka 实例没有数据库列表")
	case "redis":
		return nil, fmt.Errorf("Redis 实例请使用 Redis 查询页")
	default:
		return nil, fmt.Errorf("暂不支持探测该实例类型: %s", inst.DBType)
	}
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]Item, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, Item{Name: n, System: IsSystem(n)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func listMySQL(ctx context.Context, host string, port int, user, password string) ([]string, error) {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.Timeout = 8 * time.Second
	cfg.ReadTimeout = 8 * time.Second
	cfg.Params = map[string]string{"charset": "utf8mb4", "parseTime": "true"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetConnMaxLifetime(15 * time.Second)
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNames(rows)
}

func listPostgres(ctx context.Context, host string, port int, user, password, defaultDB string) ([]string, error) {
	if defaultDB == "" {
		defaultDB = "postgres"
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + defaultDB,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	q.Set("connect_timeout", "8")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetConnMaxLifetime(15 * time.Second)
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT datname FROM pg_database WHERE datallowconn ORDER BY datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNames(rows)
}

func listClickHouse(ctx context.Context, host string, port int, user, password string) ([]string, error) {
	httpPort := port
	switch port {
	case 9000, 9009, 9440:
		httpPort = 8123
	}
	u := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host, httpPort),
	}
	q := u.Query()
	q.Set("query", "SHOW DATABASES")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Key", password)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("ClickHouse 探测失败: %s", msg)
	}
	var names []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func scanNames(rows *sql.Rows) ([]string, error) {
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
