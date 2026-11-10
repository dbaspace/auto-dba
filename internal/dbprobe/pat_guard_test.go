package dbprobe

import "testing"

func TestPATQueryBlocked(t *testing.T) {
	allowed := []string{
		"SELECT id FROM t LIMIT 10",
		"SELECT 'pg_read_file' AS x LIMIT 1",
		"SHOW TABLES",
		"SHOW FULL COLUMNS FROM t",
		"SHOW CREATE TABLE t",
		"DESC t",
	}
	for _, sql := range allowed {
		if _, msg, blocked := PATQueryBlocked([]string{sql}); blocked {
			t.Fatalf("%s blocked: %s", sql, msg)
		}
	}
	denied := map[string]string{
		"SHOW GRANTS":                                patShowDeniedHint,
		"SHOW CREATE USER app":                       patShowDeniedHint,
		"SELECT * FROM pg_authid LIMIT 1":            patSecretReadHint,
		"SELECT * FROM pg_shadow LIMIT 1":            patSecretReadHint,
		"SELECT * FROM mysql.user LIMIT 1":           patSecretReadHint,
		"SELECT pg_read_file('a') LIMIT 1":           patSecretReadHint,
		"SELECT load_file('/etc/passwd') LIMIT 1":    patSecretReadHint,
		"SELECT dblink_exec('c','select 1') LIMIT 1": patSecretReadHint,
		`SELECT * FROM "tbl_users" LIMIT 1`:          patMetaTableHint,
		"SELECT password FROM tbl_instances LIMIT 1": patMetaTableHint,
		"SELECT * FROM tbl_nacos_admin LIMIT 1":      patMetaTableHint,
		"SELECT * FROM tbl_db_accounts LIMIT 1":      patMetaTableHint,
	}
	for sql, want := range denied {
		_, msg, blocked := PATQueryBlocked([]string{sql})
		if !blocked || msg != want {
			t.Fatalf("%s got blocked=%v msg=%q want %q", sql, blocked, msg, want)
		}
	}
	_, msg, blocked := PATQueryBlocked([]string{"SHOW TABLES", "SHOW GRANTS"})
	if !blocked || msg == "" {
		t.Fatal("second statement")
	}
}

func TestPATStarRocksShow(t *testing.T) {
	allowed := []string{
		"SHOW TABLET FROM db_ods.ods_tbl_coupon_code WHERE indexname = 'ods_tbl_coupon_code' ORDER BY TabletId",
		"SHOW PARTITIONS FROM ods_tbl_coupon_code",
		"SHOW BACKENDS",
		"SHOW PROC '/frontends'",
		"SHOW ROUTINE LOAD",
		"SHOW DATA FROM t",
		"SHOW TEMPORARY PARTITIONS FROM t",
		"SHOW COMPUTE NODES",
		"SHOW CREATE MATERIALIZED VIEW mv",
		"SHOW RUNNING QUERIES",
		"SHOW SESSION VARIABLES",
		"SHOW GLOBAL VARIABLES",
		"SHOW STATS META",
		"SHOW HISTOGRAM META",
		"SHOW ANALYZE JOB",
		"ADMIN SHOW CONFIG",
		"ADMIN SHOW REPLICA STATUS FROM t",
	}
	for _, sql := range allowed {
		if _, msg, blocked := PATQueryBlocked([]string{sql}); blocked {
			t.Fatalf("%s blocked before engine check: %s", sql, msg)
		}
		if _, msg, blocked := PATStarRocksShowOnly([]string{sql}, "starrocks"); blocked {
			t.Fatalf("starrocks %s blocked: %s", sql, msg)
		}
		if _, msg, blocked := PATStarRocksShowOnly([]string{sql}, "mysql"); !blocked || msg != patShowStarRocksHint {
			t.Fatalf("mysql %s got blocked=%v msg=%q", sql, blocked, msg)
		}
	}
	if _, _, blocked := PATStarRocksShowOnly([]string{"SHOW TABLES"}, "mysql"); blocked {
		t.Fatal("SHOW TABLES is not starrocks-only")
	}
	denied := []string{
		"SHOW TABLET FROM t INTO OUTFILE '/tmp/a'",
		"SHOW REPOSITORIES",
		"SHOW CREATE STORAGE VOLUME v",
		"SHOW STORAGE VOLUMES",
		"SHOW USERS",
		"SHOW ROLES",
		"SHOW PROPERTY",
		"SHOW AUTHENTICATION",
		"SHOW WHITELIST",
		"SHOW SQLBLACKLIST",
		"ADMIN SHOW CONFIG INTO OUTFILE '/tmp/a'",
	}
	for _, sql := range denied {
		if _, msg, blocked := PATQueryBlocked([]string{sql}); !blocked || msg != patShowDeniedHint {
			t.Fatalf("%s got blocked=%v msg=%q", sql, blocked, msg)
		}
	}
	if err := IsReadOnlySQL("ADMIN SET FRONTEND CONFIG ('x' = 'y')"); err == nil {
		t.Fatal("ADMIN SET must stay rejected")
	}
	if err := IsReadOnlySQL("ADMIN SHOW CONFIG"); err != nil {
		t.Fatal(err)
	}
}
