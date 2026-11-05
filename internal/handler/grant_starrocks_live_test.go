package handler

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"auto-dba/internal/config"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
	"auto-dba/internal/ticketexec"
)

// 连测试 StarRocks 验证授权语句。默认跳过：SR_GRANT_LIVE=1 go test -run TestStarRocksGrantLive
func TestStarRocksGrantLive(t *testing.T) {
	if os.Getenv("SR_GRANT_LIVE") == "" {
		t.Skip("set SR_GRANT_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for i := 0; i < 6; i++ {
		if _, err := os.Stat("config/config.yaml"); err == nil {
			break
		}
		if err := os.Chdir(".."); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.Open(cfg.DB.Driver, cfg.DB.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Close()

	insts, err := meta.ListInstances("starrocks", "10.100.112.14")
	if err != nil {
		t.Fatal(err)
	}
	var inst *store.Instance
	for i := range insts {
		it := &insts[i]
		blob := strings.ToLower(it.Host + " " + it.DomainName + " " + strings.Join(it.Domains, " "))
		if strings.Contains(blob, "10.100.112.14") {
			inst = it
			break
		}
	}
	if inst == nil {
		t.Fatal("未找到 10.100.112.14 的 StarRocks 实例")
	}
	pwd, err := secret.Decrypt(cfg.FieldCrypto(), inst.Password)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimSpace(inst.Host)
	if host == "" {
		host = "10.100.112.14"
	}
	t.Logf("instance=%s host=%s:%d user=%s", inst.Name, host, inst.Port, inst.Username)

	admin := ticketexec.Target{
		Host: host, Port: inst.Port, Username: inst.Username, Password: pwd, DBType: "starrocks",
	}
	db, err := openLiveMySQL(admin)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var ver string
	if err := db.QueryRowContext(ctx, "SELECT current_version()").Scan(&ver); err != nil {
		t.Fatal(err)
	}
	t.Logf("version=%s", ver)

	const (
		dbName  = "db_autodba_srgrant_tmp"
		userAll = "autodba_srgrant_all"
		userRO  = "autodba_srgrant_ro"
	)
	passAll := randomAccountPassword(16)
	passRO := randomAccountPassword(16)
	cleanup := func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ccancel()
		for _, u := range []string{userAll, userRO} {
			_, _ = db.ExecContext(cctx, "DROP USER "+starRocksUserIdent(u))
		}
		_, _ = db.ExecContext(cctx, "DROP DATABASE IF EXISTS "+dbName)
	}
	cleanup()
	defer cleanup()

	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatal(err)
	}

	exists, err := ticketexec.StarRocksUserExists(ctx, admin, userAll)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("新建前用户不应存在")
	}

	if err := liveExec(ctx, t, admin, buildStarRocksCreateUserSQL(userAll, passAll)); err != nil {
		t.Fatal(err)
	}
	exists, err = ticketexec.StarRocksUserExists(ctx, admin, userAll)
	if err != nil || !exists {
		t.Fatalf("建用户后应存在: exists=%v err=%v", exists, err)
	}
	again := ticketexec.ExecStatements(ctx, admin, []string{buildStarRocksCreateUserSQL(userAll, passAll)})
	if again == nil || again.Success || !execAlreadyExists(again) {
		t.Fatalf("重复 CREATE USER 应报已存在: %+v", again)
	}
	t.Log("重复 CREATE USER 识别为已存在")

	allPerms := []string{"r", "c", "u", "d", "ddl", "export"}
	for _, stmt := range renderStarRocksGrantStatements(userAll, dbName, allPerms) {
		if err := liveExec(ctx, t, admin, stmt); err != nil {
			t.Fatalf("空库授权失败: %v\n%s", err, stmt)
		}
	}
	grants := showGrants(ctx, t, db, userAll)
	t.Logf("SHOW GRANTS all:\n%s", grants)
	assertGrantLine(t, grants, "ALL TABLES IN DATABASE "+dbName, []string{"DELETE", "DROP", "INSERT", "SELECT", "ALTER", "EXPORT", "UPDATE"})
	assertGrantLine(t, grants, "ALL VIEWS IN DATABASE "+dbName, []string{"SELECT", "DROP"})
	assertGrantLine(t, grants, "ALL MATERIALIZED VIEWS IN DATABASE "+dbName, []string{"ALTER", "REFRESH", "DROP", "SELECT"})
	assertGrantLine(t, grants, "ON DATABASE "+dbName, []string{"CREATE TABLE", "DROP", "ALTER", "CREATE VIEW", "CREATE FUNCTION", "CREATE MATERIALIZED VIEW", "CREATE PIPE"})

	if err := liveExec(ctx, t, admin, buildStarRocksCreateUserSQL(userRO, passRO)); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range renderStarRocksGrantStatements(userRO, dbName, []string{"r"}) {
		if err := liveExec(ctx, t, admin, stmt); err != nil {
			t.Fatal(err)
		}
	}
	roGrants := showGrants(ctx, t, db, userRO)
	t.Logf("SHOW GRANTS ro:\n%s", roGrants)
	assertGrantLine(t, roGrants, "ALL TABLES IN DATABASE "+dbName, []string{"SELECT"})
	if strings.Contains(strings.ToUpper(roGrants), "CREATE TABLE") {
		t.Fatal("只读账号不应有 CREATE TABLE")
	}

	if _, err := db.ExecContext(ctx, "CREATE TABLE "+dbName+".t_probe(id INT) PROPERTIES(\"replication_num\"=\"1\")"); err != nil {
		t.Fatal(err)
	}
	ro := admin
	ro.Username = userRO
	ro.Password = passRO
	roDB, err := openLiveMySQL(ro)
	if err != nil {
		t.Fatal(err)
	}
	defer roDB.Close()
	var n int
	if err := roDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+dbName+".t_probe").Scan(&n); err != nil {
		t.Fatalf("只读账号查询失败: %v", err)
	}
	if _, err := roDB.ExecContext(ctx, "DROP TABLE "+dbName+".t_probe"); err == nil {
		t.Fatal("只读账号不应能 DROP TABLE")
	} else {
		t.Logf("只读 DROP 被拒绝: %v", err)
	}
	t.Log("PASS")
}

func openLiveMySQL(t ticketexec.Target) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User = t.Username
	cfg.Passwd = t.Password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", t.Host, t.Port)
	cfg.Timeout = 8 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	return db, nil
}

func liveExec(ctx context.Context, t *testing.T, admin ticketexec.Target, stmt string) error {
	res := ticketexec.ExecStatements(ctx, admin, []string{stmt})
	if err := execOKOrExists(res); err != nil {
		return err
	}
	logged := stmt
	if i := strings.Index(strings.ToUpper(logged), " IDENTIFIED BY "); i >= 0 {
		logged = logged[:i] + " IDENTIFIED BY '***'"
	}
	t.Logf("ok: %s", logged)
	return nil
}

func showGrants(ctx context.Context, t *testing.T, db *sql.DB, user string) string {
	q := "SHOW GRANTS FOR " + starRocksUserIdent(user)
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		var parts []string
		for _, v := range vals {
			if v.Valid {
				parts = append(parts, v.String)
			}
		}
		lines = append(lines, strings.Join(parts, " | "))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func assertGrantLine(t *testing.T, grants, object string, privs []string) {
	t.Helper()
	var line string
	for _, l := range strings.Split(grants, "\n") {
		if strings.Contains(strings.ToUpper(l), strings.ToUpper(object)) {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("授权结果缺少 %s\n%s", object, grants)
	}
	head := line
	if on := strings.Index(strings.ToUpper(line), " ON "); on >= 0 {
		head = line[:on]
	}
	headU := strings.ToUpper(head)
	for _, p := range privs {
		if !strings.Contains(headU, p) {
			t.Fatalf("%s 缺少权限 %s\n%s", object, p, line)
		}
	}
}
