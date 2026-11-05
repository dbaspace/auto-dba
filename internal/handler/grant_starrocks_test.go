package handler

import (
	"reflect"
	"strings"
	"testing"
)

func TestRenderStarRocksGrantStatements_All(t *testing.T) {
	got := renderStarRocksGrantStatements("member_user", "db_member_ai", []string{"r", "c", "u", "d", "ddl", "export"})
	want := []string{
		"GRANT DELETE, DROP, INSERT, SELECT, ALTER, EXPORT, UPDATE ON ALL TABLES IN DATABASE db_member_ai TO USER 'member_user'@'%'",
		"GRANT SELECT, DROP ON ALL VIEWS IN DATABASE db_member_ai TO USER 'member_user'@'%'",
		"GRANT ALTER, REFRESH, DROP, SELECT ON ALL MATERIALIZED VIEWS IN DATABASE db_member_ai TO USER 'member_user'@'%'",
		"GRANT CREATE TABLE, DROP, ALTER, CREATE VIEW, CREATE FUNCTION, CREATE MATERIALIZED VIEW, CREATE PIPE ON DATABASE db_member_ai TO USER 'member_user'@'%'",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%s", strings.Join(got, "\n"))
	}
}

func TestRenderStarRocksGrantStatements_ReadOnly(t *testing.T) {
	got := renderStarRocksGrantStatements("u", "db1", []string{"r"})
	want := []string{
		"GRANT SELECT ON ALL TABLES IN DATABASE db1 TO USER 'u'@'%'",
		"GRANT SELECT ON ALL VIEWS IN DATABASE db1 TO USER 'u'@'%'",
		"GRANT SELECT ON ALL MATERIALIZED VIEWS IN DATABASE db1 TO USER 'u'@'%'",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%s", strings.Join(got, "\n"))
	}
}

func TestRenderStarRocksGrantStatements_DDLOnly(t *testing.T) {
	got := renderStarRocksGrantStatements("u", "db1", []string{"ddl"})
	if len(got) != 4 {
		t.Fatalf("want 4 stmts, got %d: %v", len(got), got)
	}
	if got[0] != "GRANT DROP, ALTER ON ALL TABLES IN DATABASE db1 TO USER 'u'@'%'" {
		t.Fatalf("tables: %s", got[0])
	}
}

func TestRenderStarRocksGrantStatements_QuoteUser(t *testing.T) {
	got := renderStarRocksGrantStatements("o'k", "db1", []string{"c"})
	if len(got) != 1 || !strings.HasSuffix(got[0], "TO USER 'o''k'@'%'") {
		t.Fatalf("got: %v", got)
	}
}

func TestBuildCreateUserSQL_StarRocks(t *testing.T) {
	got := buildCreateUserSQL("starrocks", "report", "p'wd")
	if got != "CREATE USER 'report'@'%' IDENTIFIED BY 'p''wd'" {
		t.Fatalf("got: %s", got)
	}
}

func TestBuildSchemaGrantSQL_StarRocksUsesNewSyntax(t *testing.T) {
	got := buildSchemaGrantSQL("starrocks", "u", "db1", "", []string{"r", "c"})
	if strings.Contains(got, "ON DATABASE db1 TO u") || !strings.Contains(got, "ON ALL TABLES IN DATABASE db1") {
		t.Fatalf("got: %s", got)
	}
}

func TestNormalizeAccountPerms(t *testing.T) {
	if got := normalizeAccountPerms("postgres", []string{"r", "ddl", "export", "r"}); !reflect.DeepEqual(got, []string{"r"}) {
		t.Fatalf("postgres: %v", got)
	}
	if got := normalizeAccountPerms("starrocks", []string{"r", "ddl", "export"}); !reflect.DeepEqual(got, []string{"r", "ddl", "export"}) {
		t.Fatalf("starrocks: %v", got)
	}
}

// PG 收到 StarRocks 专属权限时输出与只传 r 完全一致。
func TestPGIgnoresStarRocksPerms(t *testing.T) {
	a := buildSchemaGrantSQL("postgres", "app_user", "db_new", "owner_x", []string{"r", "ddl", "export"})
	b := buildSchemaGrantSQL("postgres", "app_user", "db_new", "owner_x", []string{"r"})
	if a != b {
		t.Fatalf("pg differs:\n%s\n---\n%s", a, b)
	}
}

func TestRenderStarRocksDCLScript(t *testing.T) {
	got := renderStarRocksDCLScript([]string{"a", "b"}, "u", []string{"c"})
	want := "GRANT INSERT ON ALL TABLES IN DATABASE a TO USER 'u'@'%';\nGRANT INSERT ON ALL TABLES IN DATABASE b TO USER 'u'@'%';"
	if got != want {
		t.Fatalf("got: %s", got)
	}
}
