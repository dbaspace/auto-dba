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
	if err := IsReadOnlySQL("SELECT 'insert into t' AS txt"); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x",
		"SELECT * INTO new_t FROM old_t",
		"EXPLAIN ANALYZE INSERT INTO t VALUES (1)",
	} {
		if err := IsReadOnlySQL(sql); err == nil {
			t.Fatalf("expected deny nested write: %s", sql)
		}
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

func TestMissingPATSelectLimit(t *testing.T) {
	if _, missing := MissingPATSelectLimit([]string{"SELECT * FROM t"}); !missing {
		t.Fatal("expected missing LIMIT")
	}
	if _, missing := MissingPATSelectLimit([]string{"SELECT * FROM (SELECT * FROM t LIMIT 10) x"}); !missing {
		t.Fatal("subquery LIMIT must not count")
	}
	if _, missing := MissingPATSelectLimit([]string{"SELECT * FROM t LIMIT 0"}); !missing {
		t.Fatal("LIMIT 0 is not a positive count")
	}
	for _, sql := range []string{
		"SELECT * FROM t LIMIT 100",
		"select * from t limit 10;",
		"SELECT * FROM t LIMIT 10 OFFSET 5",
		"SELECT * FROM t LIMIT 20, 5",
		"WITH c AS (SELECT 1 AS a) SELECT * FROM c LIMIT 10",
		"SHOW TABLES",
		"EXPLAIN SELECT * FROM t",
		"DESC t",
	} {
		if _, missing := MissingPATSelectLimit([]string{sql}); missing {
			t.Fatalf("should allow: %s", sql)
		}
	}
	idx, missing := MissingPATSelectLimit([]string{"SHOW TABLES", "SELECT * FROM t"})
	if !missing || idx != 1 {
		t.Fatalf("idx=%d missing=%v", idx, missing)
	}
	msg := PATSelectLimitMessage(0, 1)
	if msg != "PAT 查询的 SELECT 必须在语句末尾写 LIMIT，例如 LIMIT 100" {
		t.Fatalf("msg=%s", msg)
	}
	if !strings.Contains(PATSelectLimitMessage(1, 2), "第 2 条") {
		t.Fatal(PATSelectLimitMessage(1, 2))
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
