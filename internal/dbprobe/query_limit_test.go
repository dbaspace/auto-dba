package dbprobe

import (
	"strings"
	"testing"
)

func TestIsReadOnlySQL(t *testing.T) {
	if err := IsReadOnlySQL("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if err := IsReadOnlySQL("with x as (select 1) select * from x"); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"TRUNCATE TABLE t",
		"DELETE FROM t",
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a=1",
		"DROP TABLE t",
		"ALTER TABLE t ADD c int",
		"CREATE TABLE t(a int)",
	} {
		if err := IsReadOnlySQL(sql); err == nil {
			t.Fatalf("expected deny: %s", sql)
		} else if !strings.Contains(err.Error(), "禁止") {
			t.Fatalf("msg=%v sql=%s", err, sql)
		}
	}
}

func TestEnsureQueryLimit(t *testing.T) {
	got := EnsureQueryLimit("SELECT * FROM t", 1000, 10000)
	if !strings.HasSuffix(strings.TrimSuffix(got.SQL, ";"), "limit 1000") {
		t.Fatalf("inject: %q", got.SQL)
	}
	if got.AppliedLimit != 1000 || got.MaxLimit != 10000 {
		t.Fatalf("%+v", got)
	}

	got = EnsureQueryLimit("SELECT * FROM t LIMIT 50", 1000, 10000)
	if got.AppliedLimit != 50 || !strings.Contains(got.SQL, "limit 50") {
		t.Fatalf("keep user: %q applied=%d", got.SQL, got.AppliedLimit)
	}
	if strings.Contains(got.SQL, "LIMIT") {
		t.Fatalf("should lowercase: %q", got.SQL)
	}

	got = EnsureQueryLimit("SELECT * FROM t LIMIT 5000", 1000, 1000)
	if got.AppliedLimit != 1000 || !strings.Contains(got.SQL, "limit 1000") {
		t.Fatalf("clamp: %q applied=%d", got.SQL, got.AppliedLimit)
	}

	got = EnsureQueryLimit("SHOW TABLES", 1000, 10000)
	if got.SQL != "SHOW TABLES" || got.AppliedLimit != 10000 {
		t.Fatalf("show: %+v", got)
	}

	got = EnsureQueryLimit("WITH c AS (SELECT 1 AS a) SELECT * FROM c", 100, 200)
	if !strings.Contains(got.SQL, "limit 100") {
		t.Fatalf("with: %q", got.SQL)
	}
}

func TestRequireExportSQLLimit(t *testing.T) {
	if _, err := RequireExportSQLLimit("select * from t"); err == nil {
		t.Fatal("expected reject without LIMIT")
	}
	n, err := RequireExportSQLLimit("select * from t LIMIT 280000")
	if err != nil || n != 280000 {
		t.Fatalf("got n=%d err=%v", n, err)
	}
	n, err = RequireExportSQLLimit("select * from t limit 50;")
	if err != nil || n != 50 {
		t.Fatalf("trailing semi n=%d err=%v", n, err)
	}
	n, err = RequireExportSQLLimit("select * from t LIMIT 2000000")
	if err != nil || n != exportLimitSanityMax {
		t.Fatalf("clamp n=%d err=%v", n, err)
	}
}

func TestEnsureExportLimit(t *testing.T) {
	got := EnsureExportLimit("SELECT * FROM t LIMIT 100", 500000, 500000)
	if got.AppliedLimit != 100 || !strings.Contains(got.SQL, "limit 100") {
		t.Fatalf("keep user limit: %+v", got)
	}
}

func TestSplitReadOnlyStatements(t *testing.T) {
	parts, err := SplitReadOnlyStatements("SELECT 1; SELECT 2", 3)
	if err != nil || len(parts) != 2 {
		t.Fatalf("%v %v", parts, err)
	}
	if _, err := SplitReadOnlyStatements("SELECT 1; DELETE FROM t", 3); err == nil {
		t.Fatal("expected deny delete")
	}
	if _, err := SplitReadOnlyStatements("SELECT 1; SELECT 2; SELECT 3; SELECT 4", 3); err == nil {
		t.Fatal("expected max 3")
	}
}
