package handler

import (
	"strings"
	"testing"
)

func TestUnionApproverNames(t *testing.T) {
	got := unionApproverNames([]string{"admin,dba"}, []string{"admin", "dba", "mayu"})
	want := []string{"admin", "dba", "mayu"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if first := unionApproverNames([]string{" admin，dba "}); len(first) != 2 || first[0] != "admin" || first[1] != "dba" {
		t.Fatalf("split got %v", first)
	}
}

func TestGrantPrivileges(t *testing.T) {
	got := grantPrivileges([]string{"r", "c", "u"})
	if got != "SELECT, INSERT, UPDATE" {
		t.Fatalf("got %q", got)
	}
	if grantPrivileges(nil) != "SELECT" {
		t.Fatal("empty perms should default SELECT")
	}
}

func TestBuildCreateUserSQL(t *testing.T) {
	pg := buildCreateUserSQL("postgres", "test_abc", "p'wd")
	if pg != `CREATE USER "test_abc" ENCRYPTED PASSWORD 'p''wd'` {
		t.Fatalf("pg: %s", pg)
	}
	my := buildCreateUserSQL("mysql", "u1", "x")
	if my != "CREATE USER u1 IDENTIFIED BY 'x'" {
		t.Fatalf("mysql: %s", my)
	}
}

func TestAlreadyExistsErr(t *testing.T) {
	if !alreadyExistsErr(`pq: database "db_x" already exists`) {
		t.Fatal("pg already exists")
	}
	if alreadyExistsErr("connection refused") {
		t.Fatal("other errors should not match")
	}
}

func TestRandomAccountPassword(t *testing.T) {
	for i := 0; i < 50; i++ {
		p := randomAccountPassword(16)
		if len(p) != 16 {
			t.Fatalf("len=%d want 16, pwd=%q", len(p), p)
		}
		var hasLower, hasUpper, hasDigit bool
		for _, c := range p {
			switch {
			case c >= 'a' && c <= 'z':
				hasLower = true
			case c >= 'A' && c <= 'Z':
				hasUpper = true
			case c >= '0' && c <= '9':
				hasDigit = true
			default:
				t.Fatalf("special char %q in %q", c, p)
			}
		}
		if !hasLower || !hasUpper || !hasDigit {
			t.Fatalf("missing class in %q lower=%v upper=%v digit=%v", p, hasLower, hasUpper, hasDigit)
		}
	}
}

func TestBuildSchemaGrantSQL_PGAlignedWithDCL(t *testing.T) {
	got := buildSchemaGrantSQL("postgres", "app_user", "db_new", "qtx_postgres", []string{"r", "c"})
	if got == "" {
		t.Fatal("empty sql")
	}
	if strings.Contains(got, "CREATE ON SCHEMA") || strings.Contains(got, "USAGE, CREATE") {
		t.Fatalf("schema CREATE forbidden: %s", got)
	}
	if !strings.Contains(got, `GRANT USAGE ON SCHEMA public TO "app_user"`) {
		t.Fatalf("missing USAGE: %s", got)
	}
	if !strings.Contains(got, `ALTER DEFAULT PRIVILEGES FOR ROLE "qtx_postgres" IN SCHEMA public GRANT SELECT, INSERT ON TABLES TO "app_user"`) {
		t.Fatalf("missing FOR ROLE + perms: %s", got)
	}
	if strings.Contains(got, "UPDATE") || strings.Contains(got, "DELETE") {
		t.Fatalf("perms should follow checkbox: %s", got)
	}
	// 与 DCL 同函数生成应一致
	want := strings.Join(buildPGDCLInDBStatements("app_user", "qtx_postgres", []string{"r", "c"}), ";\n")
	if got != want {
		t.Fatalf("must match DCL stmts\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildSchemaGrantSQL_PGEmptyOwner(t *testing.T) {
	if buildSchemaGrantSQL("postgres", "u", "db", "", []string{"r"}) != "" {
		t.Fatal("empty owner must yield empty sql")
	}
}

func TestBuildSchemaGrantSQL_MySQL(t *testing.T) {
	got := buildSchemaGrantSQL("mysql", "u", "db1", "", []string{"r", "c"})
	if got != "GRANT SELECT, INSERT ON DATABASE db1 TO u" {
		t.Fatalf("mysql: %s", got)
	}
}

func TestCreateDBProvisionGrantTemplate_5x(t *testing.T) {
	for i := 1; i <= 5; i++ {
		user := "u" + string(rune('a'+i-1)) + "_user"
		owner := "own_" + string(rune('0'+i))
		db := "db_" + string(rune('0'+i))
		sql := buildSchemaGrantSQL("postgres", user, db, owner, []string{"r", "d"})
		connect := buildConnectGrantSQL("postgres", user, db)
		create := buildCreateUserSQL("postgres", user, "Aa1PasswordXXXX")
		if !strings.Contains(create, "ENCRYPTED PASSWORD") {
			t.Fatalf("[%d] create: %s", i, create)
		}
		if !strings.Contains(connect, "GRANT CONNECT") {
			t.Fatalf("[%d] connect: %s", i, connect)
		}
		if strings.Contains(sql, "CREATE ON SCHEMA") {
			t.Fatalf("[%d] no schema CREATE: %s", i, sql)
		}
		if strings.Count(sql, "ALTER DEFAULT PRIVILEGES FOR ROLE") != 2 {
			t.Fatalf("[%d] want 2 FOR ROLE ALTER: %s", i, sql)
		}
	}
}
