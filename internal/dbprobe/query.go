package dbprobe

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v4/stdlib"

	"auto-dba/internal/store"
)

// DefaultExportLimit / MaxExportLimit：导出扫描上限回退与展示用硬顶（代码常量，非全局参数）。
const DefaultExportLimit = 500000
const MaxExportLimit = 1000000

type readOnlyKey struct{}

// WithReadOnlySession 让本次查询在会话上打开只读（PG default_transaction_read_only / MySQL transaction_read_only）。
func WithReadOnlySession(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, readOnlyKey{}, true)
}

func readOnlySession(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(readOnlyKey{}).(bool)
	return v
}

type QueryResult struct {
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	Truncated    bool     `json:"truncated"`
	ElapsedMS    int64    `json:"elapsed_ms"`
	Limit        int      `json:"limit,omitempty"`         // 最大查询（硬顶，文案 M）
	AppliedLimit int      `json:"applied_limit,omitempty"` // 本语句实际 limit
	ValidSQL     string   `json:"valid_sql,omitempty"`     // 改写后 SQL
}

func instConn(inst *store.Instance) (host, user, dbType string, err error) {
	if inst == nil {
		return "", "", "", fmt.Errorf("实例不存在")
	}
	host = strings.TrimSpace(inst.Host)
	if host == "" {
		host = strings.TrimSpace(inst.DomainName)
	}
	if host == "" || inst.Port <= 0 {
		return "", "", "", fmt.Errorf("实例未配置连接主机或端口")
	}
	user = strings.TrimSpace(inst.Username)
	if user == "" {
		return "", "", "", fmt.Errorf("实例未配置账号")
	}
	return host, user, strings.ToLower(strings.TrimSpace(inst.DBType)), nil
}

func IsReadOnlySQL(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fmt.Errorf("SQL 不能为空")
	}
	rest := strings.TrimSpace(strings.TrimSuffix(s, ";"))
	if strings.Contains(rest, ";") {
		return fmt.Errorf("不支持一次执行多条 SQL")
	}
	kw := firstSQLKeyword(rest)
	switch kw {
	case "SELECT", "WITH", "SHOW", "EXPLAIN", "DESC", "DESCRIBE":
		return nil
	case "TRUNCATE", "INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE",
		"REPLACE", "MERGE", "GRANT", "REVOKE", "CALL", "DO", "LOAD", "COPY",
		"VACUUM", "ANALYZE", "REINDEX", "CLUSTER", "COMMENT", "SET", "RESET",
		"BEGIN", "COMMIT", "ROLLBACK", "LOCK", "UNLOCK", "PURGE", "RENAME":
		return fmt.Errorf("查询接口仅允许只读语句，禁止 %s/DDL/DML", kw)
	default:
		if kw == "" {
			return fmt.Errorf("查询接口仅允许只读语句，禁止 DDL/DML")
		}
		return fmt.Errorf("查询接口仅允许只读语句，禁止 %s/DDL/DML", kw)
	}
}

func firstSQLKeyword(s string) string {
	s = strings.TrimSpace(s)
	for s != "" {
		if strings.HasPrefix(s, "--") {
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = strings.TrimSpace(s[i+1:])
				continue
			}
			return ""
		}
		if strings.HasPrefix(s, "/*") {
			if i := strings.Index(s, "*/"); i >= 0 {
				s = strings.TrimSpace(s[i+2:])
				continue
			}
			return ""
		}
		break
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) {
			b.WriteRune(unicode.ToUpper(r))
			continue
		}
		break
	}
	return b.String()
}

func clampLimit(n int) int {
	if n <= 0 {
		return DefaultQueryLimit
	}
	if n > MaxQueryLimit {
		return MaxQueryLimit
	}
	return n
}

func clampExportLimit(n int) int {
	if n <= 0 {
		return DefaultExportLimit
	}
	if n > exportLimitSanityMax {
		return exportLimitSanityMax
	}
	return n
}

type TableMeta struct {
	Name          string `json:"name"`
	TableName     string `json:"table_name"`
	EstimatedRows int64  `json:"estimated_rows"`
	TotalBytes    int64  `json:"total_bytes"`
}

func namesToTableMeta(names []string) []TableMeta {
	out := make([]TableMeta, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		out = append(out, TableMeta{Name: n, TableName: n})
	}
	return out
}

func ListTablesMeta(ctx context.Context, inst *store.Instance, password, dbName string) ([]TableMeta, error) {
	names, err := ListTables(ctx, inst, password, dbName)
	if err != nil {
		return nil, err
	}
	host, user, dbType, err := instConn(inst)
	if err != nil {
		return namesToTableMeta(names), nil
	}
	var meta []TableMeta
	switch family(dbType) {
	case "mysql":
		meta, err = listMySQLTablesMeta(ctx, host, inst.Port, user, password, dbName)
	case "postgres":
		meta, err = listPostgresTablesMeta(ctx, host, inst.Port, user, password, dbName)
	default:
		return namesToTableMeta(names), nil
	}
	if err != nil || len(meta) == 0 {
		return namesToTableMeta(names), nil
	}
	return meta, nil
}

func ListTables(ctx context.Context, inst *store.Instance, password, dbName string) ([]string, error) {
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return nil, fmt.Errorf("库名不能为空")
	}
	host, user, dbType, err := instConn(inst)
	if err != nil {
		return nil, err
	}
	switch family(dbType) {
	case "postgres":
		return listPostgresTables(ctx, host, inst.Port, user, password, dbName)
	case "mysql":
		return listMySQLTables(ctx, host, inst.Port, user, password, dbName)
	case "clickhouse":
		return listClickHouseTables(ctx, host, inst.Port, user, password, dbName)
	case "sqlserver":
		return listSQLServerTables(ctx, host, inst.Port, user, password, dbName)
	case "oracle":
		return listOracleTables(ctx, host, inst.Port, user, password, dbName)
	case "mongodb":
		return listMongoTables(ctx, host, inst.Port, user, password, dbName)
	case "elasticsearch":
		return listElasticTables(ctx, host, inst.Port, user, password, dbName)
	case "cassandra":
		return listCassandraTables(ctx, host, inst.Port, user, password, dbName)
	case "kafka":
		return nil, fmt.Errorf("Kafka 实例没有数据表")
	case "redis":
		return nil, fmt.Errorf("Redis 实例请使用 Redis 查询页")
	default:
		return nil, fmt.Errorf("暂不支持该实例类型: %s", inst.DBType)
	}
}

func ExecQuery(ctx context.Context, inst *store.Instance, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	if err := isReadOnlyQuery(inst.DBType, rawSQL); err != nil {
		return nil, err
	}
	return runQuery(ctx, inst, password, dbName, rawSQL, clampLimit(limit), 0)
}

func ExecExport(ctx context.Context, inst *store.Instance, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	if err := isReadOnlyQuery(inst.DBType, rawSQL); err != nil {
		return nil, err
	}
	// Postgres：会话级超时；大导出由 HTTP context（约 10m）主导，此处给足余量
	return runQuery(ctx, inst, password, dbName, rawSQL, clampExportLimit(limit), 9*time.Minute)
}

func runQuery(ctx context.Context, inst *store.Instance, password, dbName, rawSQL string, limit int, stmtTimeout time.Duration) (*QueryResult, error) {
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return nil, fmt.Errorf("库名不能为空")
	}
	host, user, dbType, err := instConn(inst)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	var out *QueryResult
	switch family(dbType) {
	case "postgres":
		out, err = queryPostgres(ctx, host, inst.Port, user, password, dbName, rawSQL, limit, stmtTimeout)
	case "mysql":
		out, err = queryMySQL(ctx, host, inst.Port, user, password, dbName, rawSQL, limit)
	case "clickhouse":
		out, err = queryClickHouse(ctx, host, inst.Port, user, password, dbName, rawSQL, limit)
	case "sqlserver":
		out, err = querySQLServer(ctx, host, inst.Port, user, password, dbName, rawSQL, limit)
	case "oracle":
		out, err = queryOracle(ctx, host, inst.Port, user, password, dbName, rawSQL, limit)
	case "mongodb":
		out, err = queryMongo(ctx, host, inst.Port, user, password, dbName, rawSQL, limit)
	case "elasticsearch":
		out, err = queryElastic(ctx, host, inst.Port, user, password, dbName, rawSQL, limit)
	case "cassandra":
		out, err = queryCassandra(ctx, host, inst.Port, user, password, dbName, rawSQL, limit)
	case "kafka":
		return nil, fmt.Errorf("Kafka 实例不支持 SQL 查询")
	case "redis":
		return nil, fmt.Errorf("Redis 实例请使用 Redis 查询页")
	default:
		return nil, fmt.Errorf("暂不支持该实例类型: %s", inst.DBType)
	}
	if err != nil {
		return nil, err
	}
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, nil
}

func openMySQL(host string, port int, user, password, dbName string) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", host, port)
	cfg.DBName = dbName
	cfg.Timeout = 8 * time.Second
	cfg.ReadTimeout = 20 * time.Second
	cfg.Params = map[string]string{"charset": "utf8mb4", "parseTime": "true"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(25 * time.Second)
	db.SetMaxOpenConns(1)
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
	q.Set("connect_timeout", "8")
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(25 * time.Second)
	db.SetMaxOpenConns(1)
	return db, nil
}

func listMySQLTables(ctx context.Context, host string, port int, user, password, dbName string) ([]string, error) {
	db, err := openMySQL(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = ? ORDER BY table_name`, dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNames(rows)
}

func listMySQLTablesMeta(ctx context.Context, host string, port int, user, password, dbName string) ([]TableMeta, error) {
	db, err := openMySQL(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT TABLE_NAME, COALESCE(TABLE_ROWS,0), COALESCE(DATA_LENGTH,0)+COALESCE(INDEX_LENGTH,0)
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME`, dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableMeta
	for rows.Next() {
		var t TableMeta
		if err := rows.Scan(&t.TableName, &t.EstimatedRows, &t.TotalBytes); err != nil {
			return nil, err
		}
		t.Name = t.TableName
		out = append(out, t)
	}
	return out, rows.Err()
}

func listPostgresTablesMeta(ctx context.Context, host string, port int, user, password, dbName string) ([]TableMeta, error) {
	db, err := openPostgres(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT CASE WHEN schemaname = 'public' THEN relname ELSE schemaname || '.' || relname END,
			COALESCE(n_live_tup,0), COALESCE(pg_total_relation_size(relid),0)
		FROM pg_stat_user_tables
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TableMeta
	for rows.Next() {
		var t TableMeta
		if err := rows.Scan(&t.TableName, &t.EstimatedRows, &t.TotalBytes); err != nil {
			return nil, err
		}
		t.Name = t.TableName
		out = append(out, t)
	}
	return out, rows.Err()
}

func listPostgresTables(ctx context.Context, host string, port int, user, password, dbName string) ([]string, error) {
	db, err := openPostgres(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT CASE WHEN schemaname = 'public' THEN tablename ELSE schemaname || '.' || tablename END
		FROM pg_tables
		WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNames(rows)
}

func listClickHouseTables(ctx context.Context, host string, port int, user, password, dbName string) ([]string, error) {
	q := "SHOW TABLES FROM `" + strings.ReplaceAll(dbName, "`", "") + "`"
	body, err := clickhouseHTTP(ctx, host, port, user, password, dbName, q)
	if err != nil {
		return nil, err
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

func queryMySQL(ctx context.Context, host string, port int, user, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	db, err := openMySQL(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	if readOnlySession(ctx) {
		if _, err := db.ExecContext(ctx, "SET SESSION transaction_read_only = 1"); err != nil {
			return nil, fmt.Errorf("设置 transaction_read_only: %w", err)
		}
	}
	return scanQuery(ctx, db, rawSQL, limit)
}

func queryPostgres(ctx context.Context, host string, port int, user, password, dbName, rawSQL string, limit int, stmtTimeout time.Duration) (*QueryResult, error) {
	db, err := openPostgres(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	if stmtTimeout > 0 {
		ms := int(stmtTimeout / time.Millisecond)
		if ms < 1 {
			ms = 1
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("SET statement_timeout = %d", ms)); err != nil {
			return nil, fmt.Errorf("设置 statement_timeout: %w", err)
		}
	}
	if readOnlySession(ctx) {
		if _, err := db.ExecContext(ctx, "SET default_transaction_read_only = on"); err != nil {
			return nil, fmt.Errorf("设置 default_transaction_read_only: %w", err)
		}
	}
	return scanQuery(ctx, db, rawSQL, limit)
}

func queryClickHouse(ctx context.Context, host string, port int, user, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	sqlText := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rawSQL), ";"))
	if !strings.Contains(strings.ToUpper(sqlText), " FORMAT ") {
		sqlText += " FORMAT JSON"
	}
	body, err := clickhouseHTTP(ctx, host, port, user, password, dbName, sqlText)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Meta []struct {
			Name string `json:"name"`
		} `json:"meta"`
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		lines := strings.Split(strings.TrimSpace(string(body)), "\n")
		out := &QueryResult{Columns: []string{"result"}, Rows: make([][]any, 0, len(lines))}
		for i, line := range lines {
			if i >= limit {
				out.Truncated = true
				break
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			out.Rows = append(out.Rows, []any{line})
		}
		return out, nil
	}
	cols := make([]string, 0, len(parsed.Meta))
	for _, m := range parsed.Meta {
		cols = append(cols, m.Name)
	}
	out := &QueryResult{Columns: cols, Rows: make([][]any, 0, len(parsed.Data))}
	for i, rec := range parsed.Data {
		if i >= limit {
			out.Truncated = true
			break
		}
		row := make([]any, len(cols))
		for j, c := range cols {
			row[j] = rec[c]
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func clickhouseHTTP(ctx context.Context, host string, port int, user, password, dbName, query string) ([]byte, error) {
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
	if dbName != "" {
		q.Set("database", dbName)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Key", password)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("ClickHouse 查询失败: %s", msg)
	}
	return body, nil
}

func scanQuery(ctx context.Context, db *sql.DB, rawSQL string, limit int) (*QueryResult, error) {
	rows, err := db.QueryContext(ctx, rawSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := &QueryResult{Columns: cols, Rows: [][]any{}}
	for rows.Next() {
		if len(out.Rows) >= limit {
			out.Truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]any, len(cols))
		for i, v := range vals {
			row[i] = normalizeCell(v)
		}
		out.Rows = append(out.Rows, row)
	}
	return out, rows.Err()
}

func normalizeCell(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	default:
		return t
	}
}
