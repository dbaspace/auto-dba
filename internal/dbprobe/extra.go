package dbprobe

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gocql/gocql"
	_ "github.com/microsoft/go-mssqldb"
	go_ora "github.com/sijms/go-ora/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	reTailLimit = regexp.MustCompile(`(?i)\s+LIMIT\s+(\d+)\s*;?\s*$`)
	reSelect    = regexp.MustCompile(`(?i)^\s*SELECT\b`)
	reMongoFind = regexp.MustCompile(`(?i)^\s*db\.([A-Za-z0-9_.-]+)\.find\s*\((.*)\)\s*;?\s*$`)
	reFromIdent = regexp.MustCompile(`(?i)\bFROM\s+([A-Za-z0-9_."` + "`" + `\-]+)`)
)

func rewriteSQL(fam, raw string) string {
	s := strings.TrimSpace(raw)
	switch fam {
	case "oracle":
		if m := reTailLimit.FindStringSubmatch(s); m != nil {
			return strings.TrimSpace(reTailLimit.ReplaceAllString(s, "")) + " FETCH FIRST " + m[1] + " ROWS ONLY"
		}
	case "sqlserver":
		if m := reTailLimit.FindStringSubmatch(s); m != nil && !regexp.MustCompile(`(?i)\bTOP\s+\d+`).MatchString(s) {
			body := strings.TrimSpace(reTailLimit.ReplaceAllString(s, ""))
			return reSelect.ReplaceAllString(body, "SELECT TOP "+m[1]+" ")
		}
	}
	return s
}

func isReadOnlyQuery(dbType, raw string) error {
	switch family(dbType) {
	case "mongodb":
		return isReadOnlyMongo(raw)
	case "elasticsearch":
		s := strings.TrimSpace(raw)
		if s == "" {
			return fmt.Errorf("查询不能为空")
		}
		if strings.HasPrefix(s, "{") || strings.EqualFold(firstSQLKeyword(s), "GET") {
			return nil
		}
		return IsReadOnlySQL(raw)
	default:
		return IsReadOnlySQL(raw)
	}
}

func isReadOnlyMongo(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fmt.Errorf("查询不能为空")
	}
	if strings.HasPrefix(s, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return fmt.Errorf("Mongo JSON 无法解析: %w", err)
		}
		for k := range obj {
			switch strings.ToLower(k) {
			case "find", "aggregate", "count", "distinct", "filter", "limit":
			default:
				return fmt.Errorf("仅允许 find / aggregate / count / distinct")
			}
		}
		return nil
	}
	kw := firstSQLKeyword(s)
	switch kw {
	case "SELECT", "SHOW", "EXPLAIN", "DESC", "DESCRIBE":
		return nil
	case "DB":
		return nil
	default:
		if reMongoFind.MatchString(s) {
			return nil
		}
		return fmt.Errorf("仅允许 find / SELECT / SHOW 类只读查询")
	}
}

func openSQLServer(host string, port int, user, password, dbName string) (*sql.DB, error) {
	if dbName == "" {
		dbName = "master"
	}
	u := &url.URL{
		Scheme: "sqlserver",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%d", host, port),
	}
	q := u.Query()
	q.Set("database", dbName)
	q.Set("encrypt", "disable")
	q.Set("dial+timeout", "8")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(25 * time.Second)
	db.SetMaxOpenConns(1)
	return db, nil
}

func listSQLServer(ctx context.Context, host string, port int, user, password string) ([]string, error) {
	db, err := openSQLServer(host, port, user, password, "master")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT name FROM sys.databases ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNames(rows)
}

func listSQLServerTables(ctx context.Context, host string, port int, user, password, dbName string) ([]string, error) {
	db, err := openSQLServer(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT TABLE_SCHEMA + '.' + TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE IN ('BASE TABLE', 'VIEW')
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNames(rows)
}

func querySQLServer(ctx context.Context, host string, port int, user, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	db, err := openSQLServer(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	return scanQuery(ctx, db, rewriteSQL("sqlserver", rawSQL), limit)
}

func openOracle(host string, port int, user, password, service string) (*sql.DB, error) {
	if service == "" {
		service = "ORCL"
	}
	u := go_ora.BuildUrl(host, port, service, user, password, nil)
	db, err := sql.Open("oracle", u)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(25 * time.Second)
	db.SetMaxOpenConns(1)
	return db, nil
}

func listOracle(ctx context.Context, host string, port int, user, password string) ([]string, error) {
	var last error
	for _, svc := range []string{"ORCL", "XE", "FREEPDB1", "XEPDB1"} {
		db, err := openOracle(host, port, user, password, svc)
		if err != nil {
			last = err
			continue
		}
		if err := db.PingContext(ctx); err != nil {
			last = err
			_ = db.Close()
			continue
		}
		rows, err := db.QueryContext(ctx, `SELECT name FROM v$pdbs`)
		if err != nil {
			_ = db.Close()
			return []string{svc}, nil
		}
		names, err := scanNames(rows)
		rows.Close()
		_ = db.Close()
		if err != nil || len(names) == 0 {
			return []string{svc}, nil
		}
		return names, nil
	}
	if last != nil {
		return nil, last
	}
	return nil, fmt.Errorf("无法连接 Oracle，请确认 service name / SID")
}

func listOracleTables(ctx context.Context, host string, port int, user, password, dbName string) ([]string, error) {
	db, err := openOracle(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT table_name FROM user_tables
		UNION ALL
		SELECT owner || '.' || table_name FROM all_tables
		WHERE owner NOT IN ('SYS','SYSTEM','OUTLN','DBSNMP','APPQOSSYS','AUDSYS','XDB','WMSYS','GSMADMIN_INTERNAL')
		  AND owner <> USER
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNames(rows)
}

func queryOracle(ctx context.Context, host string, port int, user, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	db, err := openOracle(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}
	return scanQuery(ctx, db, rewriteSQL("oracle", rawSQL), limit)
}

func mongoURI(host string, port int, user, password string) string {
	if user == "" {
		return fmt.Sprintf("mongodb://%s:%d/?connectTimeoutMS=8000&serverSelectionTimeoutMS=8000", host, port)
	}
	return fmt.Sprintf("mongodb://%s:%s@%s:%d/?connectTimeoutMS=8000&serverSelectionTimeoutMS=8000&authSource=admin",
		url.PathEscape(user), url.PathEscape(password), host, port)
}

func withMongo(ctx context.Context, host string, port int, user, password string, fn func(context.Context, *mongo.Client) error) error {
	cli, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI(host, port, user, password)))
	if err != nil {
		return err
	}
	defer func() { _ = cli.Disconnect(context.Background()) }()
	if err := cli.Ping(ctx, nil); err != nil {
		return err
	}
	return fn(ctx, cli)
}

func listMongo(ctx context.Context, host string, port int, user, password string) ([]string, error) {
	var names []string
	err := withMongo(ctx, host, port, user, password, func(ctx context.Context, cli *mongo.Client) error {
		var e error
		names, e = cli.ListDatabaseNames(ctx, bson.D{})
		return e
	})
	return names, err
}

func listMongoTables(ctx context.Context, host string, port int, user, password, dbName string) ([]string, error) {
	var names []string
	err := withMongo(ctx, host, port, user, password, func(ctx context.Context, cli *mongo.Client) error {
		var e error
		names, e = cli.Database(dbName).ListCollectionNames(ctx, bson.D{})
		return e
	})
	return names, err
}

func queryMongo(ctx context.Context, host string, port int, user, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	var out *QueryResult
	err := withMongo(ctx, host, port, user, password, func(ctx context.Context, cli *mongo.Client) error {
		coll, filter, lim, err := parseMongoQuery(rawSQL, limit)
		if err != nil {
			return err
		}
		if coll == "" {
			return fmt.Errorf("请指定集合，例如 db.users.find({}) 或 SELECT * FROM users")
		}
		cur, err := cli.Database(dbName).Collection(coll).Find(ctx, filter, options.Find().SetLimit(int64(lim)))
		if err != nil {
			return err
		}
		defer cur.Close(ctx)
		out = &QueryResult{Columns: []string{"_id", "doc"}, Rows: [][]any{}}
		for cur.Next(ctx) {
			if len(out.Rows) >= lim {
				out.Truncated = true
				break
			}
			var doc bson.M
			if err := cur.Decode(&doc); err != nil {
				return err
			}
			id := fmt.Sprint(doc["_id"])
			b, _ := json.Marshal(doc)
			out.Rows = append(out.Rows, []any{id, string(b)})
		}
		return cur.Err()
	})
	return out, err
}

func parseMongoQuery(raw string, limit int) (coll string, filter bson.M, lim int, err error) {
	lim = limit
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), ";"))
	if strings.HasPrefix(s, "{") {
		var obj map[string]any
		if err = json.Unmarshal([]byte(s), &obj); err != nil {
			return "", nil, 0, err
		}
		if v, ok := obj["find"].(string); ok {
			coll = v
		}
		filter = bson.M{}
		if f, ok := obj["filter"].(map[string]any); ok {
			filter = bson.M(f)
		}
		return coll, filter, lim, nil
	}
	if m := reMongoFind.FindStringSubmatch(s); m != nil {
		coll = m[1]
		filter = bson.M{}
		arg := strings.TrimSpace(m[2])
		if arg != "" && arg != "{}" {
			var f bson.M
			if err = json.Unmarshal([]byte(arg), &f); err != nil {
				return "", nil, 0, fmt.Errorf("find 参数需为 JSON: %w", err)
			}
			filter = f
		}
		return coll, filter, lim, nil
	}
	if firstSQLKeyword(s) == "SELECT" {
		if m := reFromIdent.FindStringSubmatch(s); m != nil {
			coll = strings.Trim(m[1], "`\"")
		}
		if n := reTailLimit.FindStringSubmatch(s); n != nil {
			fmt.Sscanf(n[1], "%d", &lim)
		}
		return coll, bson.M{}, lim, nil
	}
	return "", nil, 0, fmt.Errorf("无法解析 Mongo 查询")
}

func esHTTP(ctx context.Context, host string, port int, user, password, method, path string, body string) ([]byte, error) {
	if port <= 0 {
		port = 9200
	}
	u := url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", host, port), Path: path}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return nil, err
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("Elasticsearch 失败: %s", msg)
	}
	return b, nil
}

func listElastic(ctx context.Context, host string, port int, user, password string) ([]string, error) {
	q := url.Values{}
	q.Set("format", "json")
	q.Set("h", "index")
	u := url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", host, port), Path: "/_cat/indices", RawQuery: q.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Elasticsearch 探测失败: %s", strings.TrimSpace(string(raw)))
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	var names []string
	for _, r := range rows {
		if n, ok := r["index"].(string); ok && n != "" {
			names = append(names, n)
		}
	}
	return names, nil
}

func listElasticTables(_ context.Context, _ string, _ int, _ string, _ string, dbName string) ([]string, error) {
	if dbName == "" {
		return []string{}, nil
	}
	return []string{dbName}, nil
}

func queryElastic(ctx context.Context, host string, port int, user, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	s := strings.TrimSpace(rawSQL)
	if strings.HasPrefix(s, "{") {
		path := "/" + strings.Trim(dbName, "/") + "/_search"
		if dbName == "" || dbName == "_all" {
			path = "/_search"
		}
		body, err := esHTTP(ctx, host, port, user, password, http.MethodPost, path, s)
		if err != nil {
			return nil, err
		}
		return parseESSearch(body, limit)
	}
	sqlText := strings.TrimSuffix(strings.TrimSpace(s), ";")
	payload, _ := json.Marshal(map[string]any{"query": sqlText, "fetch_size": limit})
	body, err := esHTTP(ctx, host, port, user, password, http.MethodPost, "/_sql", string(payload))
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Columns []struct {
			Name string `json:"name"`
		} `json:"columns"`
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return &QueryResult{Columns: []string{"result"}, Rows: [][]any{{string(body)}}}, nil
	}
	cols := make([]string, 0, len(parsed.Columns))
	for _, c := range parsed.Columns {
		cols = append(cols, c.Name)
	}
	out := &QueryResult{Columns: cols, Rows: [][]any{}}
	for i, row := range parsed.Rows {
		if i >= limit {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func parseESSearch(body []byte, limit int) (*QueryResult, error) {
	var parsed struct {
		Hits struct {
			Hits []map[string]any `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return &QueryResult{Columns: []string{"result"}, Rows: [][]any{{string(body)}}}, nil
	}
	out := &QueryResult{Columns: []string{"_id", "doc"}, Rows: [][]any{}}
	for i, h := range parsed.Hits.Hits {
		if i >= limit {
			out.Truncated = true
			break
		}
		id, _ := h["_id"].(string)
		b, _ := json.Marshal(h["_source"])
		out.Rows = append(out.Rows, []any{id, string(b)})
	}
	return out, nil
}

func cassSession(host string, port int, user, password, keyspace string) (*gocql.Session, error) {
	cluster := gocql.NewCluster(host)
	if port > 0 {
		cluster.Port = port
	}
	cluster.Timeout = 12 * time.Second
	cluster.ConnectTimeout = 8 * time.Second
	cluster.DisableInitialHostLookup = true
	cluster.Consistency = gocql.One
	if user != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{Username: user, Password: password}
	}
	if keyspace != "" {
		cluster.Keyspace = keyspace
	}
	return cluster.CreateSession()
}

func listCassandra(ctx context.Context, host string, port int, user, password string) ([]string, error) {
	sess, err := cassSession(host, port, user, password, "")
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	iter := sess.Query(`SELECT keyspace_name FROM system_schema.keyspaces`).WithContext(ctx).Iter()
	var names []string
	var name string
	for iter.Scan(&name) {
		names = append(names, name)
	}
	return names, iter.Close()
}

func listCassandraTables(ctx context.Context, host string, port int, user, password, dbName string) ([]string, error) {
	sess, err := cassSession(host, port, user, password, "")
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	iter := sess.Query(`SELECT table_name FROM system_schema.tables WHERE keyspace_name = ?`, dbName).WithContext(ctx).Iter()
	var names []string
	var name string
	for iter.Scan(&name) {
		names = append(names, name)
	}
	return names, iter.Close()
}

func queryCassandra(ctx context.Context, host string, port int, user, password, dbName, rawSQL string, limit int) (*QueryResult, error) {
	sess, err := cassSession(host, port, user, password, dbName)
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	iter := sess.Query(strings.TrimSpace(rawSQL)).WithContext(ctx).Iter()
	cols := iter.Columns()
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.Name)
	}
	out := &QueryResult{Columns: names, Rows: [][]any{}}
	for {
		if len(out.Rows) >= limit {
			out.Truncated = true
			break
		}
		row := make(map[string]any)
		if !iter.MapScan(row) {
			break
		}
		vals := make([]any, len(names))
		for i, n := range names {
			vals[i] = normalizeCell(row[n])
		}
		out.Rows = append(out.Rows, vals)
	}
	return out, iter.Close()
}
