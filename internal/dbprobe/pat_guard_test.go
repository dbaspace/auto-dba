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
