package ticketexec

import (
	"testing"
	"time"
)

func TestIsLongRunningPGStatement(t *testing.T) {
	long := []string{
		"CREATE INDEX CONCURRENTLY idx_a ON t(a)",
		"create unique index concurrently if not exists idx_a on t(a)",
		"-- 加索引\n/* 大表 */\n  CREATE INDEX CONCURRENTLY idx_a ON t(a)",
		"REINDEX INDEX CONCURRENTLY idx_a",
		"reindex (verbose) table concurrently t",
		"DROP INDEX CONCURRENTLY IF EXISTS idx_a",
		"REFRESH MATERIALIZED VIEW CONCURRENTLY mv",
		"ALTER TABLE t VALIDATE CONSTRAINT fk_a",
	}
	for _, s := range long {
		if !IsLongRunningPGStatement(s) {
			t.Fatalf("应识别为长耗时语句: %q", s)
		}
	}
	short := []string{
		"CREATE INDEX idx_a ON t(a)",
		"ALTER TABLE t ADD COLUMN c int",
		"DROP INDEX idx_a",
		"REFRESH MATERIALIZED VIEW mv",
		"-- CREATE INDEX CONCURRENTLY idx ON t(a)\nSELECT 1",
		"",
	}
	for _, s := range short {
		if IsLongRunningPGStatement(s) {
			t.Fatalf("不应识别为长耗时语句: %q", s)
		}
	}
	if !HasLongRunningPGStatement([]string{"ALTER TABLE t ADD COLUMN c int", "CREATE INDEX CONCURRENTLY i ON t(c)"}) {
		t.Fatal("含长语句时应为 true")
	}
	if HasLongRunningPGStatement([]string{"ALTER TABLE t ADD COLUMN c int"}) {
		t.Fatal("不含长语句时应为 false")
	}
}

func TestParseCICIndexName(t *testing.T) {
	cases := map[string]string{
		"CREATE INDEX CONCURRENTLY idx_a ON t(a)":                                 "idx_a",
		"create unique index concurrently if not exists idx_a on t(a)":            "idx_a",
		"CREATE INDEX CONCURRENTLY app.idx_a ON app.t(a)":                         "app.idx_a",
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS "App"."Idx A" ON t(a)`:           `"App"."Idx A"`,
		"/* c */ CREATE INDEX CONCURRENTLY idx_a ON ONLY t(a)":                    "idx_a",
		"CREATE INDEX CONCURRENTLY ON t(a)":                                       "",
		"CREATE INDEX idx_a ON t(a)":                                              "",
		"CREATE INDEX CONCURRENTLY tbl_i18n_text_anchor_idx ON t USING btree (a)": "tbl_i18n_text_anchor_idx",
	}
	for stmt, want := range cases {
		if got := parseCICIndexName(stmt); got != want {
			t.Fatalf("%q: got %q want %q", stmt, got, want)
		}
	}
}

func TestStmtAndRunTimeout(t *testing.T) {
	long := 3 * time.Hour
	if got := stmtTimeout("CREATE INDEX CONCURRENTLY i ON t(a)", long); got != long {
		t.Fatalf("长语句单条超时: %v", got)
	}
	if got := stmtTimeout("ALTER TABLE t ADD COLUMN c int", long); got != ddlTimeout {
		t.Fatalf("普通语句单条超时应保持 ddlTimeout: %v", got)
	}
	if got := stmtTimeout("CREATE INDEX CONCURRENTLY i ON t(a)", 0); got != 0 {
		t.Fatalf("MySQL/StarRocks 不区分语句: %v", got)
	}

	cic := []string{"CREATE INDEX CONCURRENTLY i ON t(a)"}
	if got := runTimeout("postgres", cic, long); got != ddlTimeout+long {
		t.Fatalf("PG 含长语句整体超时: %v", got)
	}
	if got := runTimeout("postgres", cic, 0); got != ddlTimeout+DefaultLongStmtTimeout {
		t.Fatalf("未配置时用默认长语句超时: %v", got)
	}
	if got := runTimeout("postgres", []string{"ALTER TABLE t ADD COLUMN c int"}, long); got != ddlTimeout {
		t.Fatalf("不含长语句时整体超时不变: %v", got)
	}
	if got := runTimeout("starrocks", cic, long); got != ddlTimeout {
		t.Fatalf("非 PG 整体超时不变: %v", got)
	}
}

func TestAsyncMaxWait(t *testing.T) {
	if got := AsyncMaxWait("ALTER TABLE t ADD COLUMN c int;", 3, time.Hour); got != AsyncAutoExecMaxWait {
		t.Fatalf("不含长语句时保持原上限: %v", got)
	}
	sqlText := "ALTER TABLE t ADD COLUMN c int;\nCREATE INDEX CONCURRENTLY i ON t(c);"
	if got := AsyncMaxWait(sqlText, 3, time.Hour); got != AsyncAutoExecMaxWait+3*time.Hour {
		t.Fatalf("按数据源数追加: %v", got)
	}
	if got := AsyncMaxWait(sqlText, 0, 0); got != AsyncAutoExecMaxWait+DefaultLongStmtTimeout {
		t.Fatalf("目标数至少 1、超时取默认: %v", got)
	}
}
