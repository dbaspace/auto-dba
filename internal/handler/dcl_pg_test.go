package handler

import (
	"fmt"
	"strings"
	"testing"

	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

func TestBuildPGDCLInDBStatements_OpsTemplate(t *testing.T) {
	stmts := buildPGDCLInDBStatements("sales_user", "qtx_postgres", []string{"r", "c", "d"})
	if len(stmts) != 5 {
		t.Fatalf("want 5 stmts, got %d: %#v", len(stmts), stmts)
	}
	joined := strings.Join(stmts, "\n")
	if strings.Contains(joined, "UPDATE") {
		t.Fatalf("UPDATE must be absent when not selected: %s", joined)
	}
	if strings.Contains(joined, "USAGE, CREATE") || strings.Contains(joined, "CREATE ON SCHEMA") {
		t.Fatalf("schema CREATE must be absent: %s", joined)
	}
	want := []string{
		`GRANT SELECT, INSERT, DELETE ON ALL TABLES IN SCHEMA public TO "sales_user"`,
		`GRANT USAGE ON SCHEMA public TO "sales_user"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "qtx_postgres" IN SCHEMA public GRANT SELECT, INSERT, DELETE ON TABLES TO "sales_user"`,
		`GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO "sales_user"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "qtx_postgres" IN SCHEMA public GRANT ALL PRIVILEGES ON SEQUENCES TO "sales_user"`,
	}
	for i, w := range want {
		if stmts[i] != w {
			t.Fatalf("stmt[%d]=\n%s\nwant\n%s", i, stmts[i], w)
		}
	}
}

func TestBuildPGDCLInDBStatements_EmptyOwnerForbidden(t *testing.T) {
	if stmts := buildPGDCLInDBStatements("u", "", []string{"r"}); len(stmts) != 0 {
		t.Fatalf("empty owner must yield no stmts: %#v", stmts)
	}
}

func TestBuildPGDCLConnectSQL(t *testing.T) {
	got := buildPGDCLConnectSQL("db_sales", "sales_user")
	want := `GRANT CONNECT ON DATABASE "db_sales" TO "sales_user"`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderPGDCLScript_ExecutableAlterNoSchemaCreate(t *testing.T) {
	owners := map[string]string{
		"coverage":   "own_a",
		"db_abc_000": "own_b",
	}
	script := renderPGDCLScript([]string{"coverage", "db_abc_000"}, "app_user", owners, []string{"r", "u"})
	if !strings.Contains(script, "-- \\c coverage") || !strings.Contains(script, "-- \\c db_abc_000") {
		t.Fatalf("missing multi-db markers: %s", script)
	}
	if strings.Contains(script, "USAGE, CREATE") {
		t.Fatalf("must not have schema CREATE: %s", script)
	}
	if !strings.Contains(script, `GRANT USAGE ON SCHEMA public TO "app_user";`) {
		t.Fatalf("missing USAGE: %s", script)
	}
	for _, line := range strings.Split(script, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "ALTER DEFAULT PRIVILEGES") {
			continue
		}
		if strings.HasPrefix(trim, "--") && strings.Contains(trim, "ALTER DEFAULT PRIVILEGES") {
			t.Fatalf("ALTER must not be commented: %s", line)
		}
	}
	if !strings.Contains(script, `ALTER DEFAULT PRIVILEGES FOR ROLE "own_a" IN SCHEMA public GRANT SELECT, UPDATE ON TABLES TO "app_user";`) {
		t.Fatalf("missing executable ALTER tables: %s", script)
	}
	if !strings.Contains(script, `ALTER DEFAULT PRIVILEGES FOR ROLE "own_b" IN SCHEMA public GRANT ALL PRIVILEGES ON SEQUENCES TO "app_user";`) {
		t.Fatalf("missing executable ALTER sequences: %s", script)
	}
}

func TestRenderPGDCLScript_SkipMissingOwner(t *testing.T) {
	script := renderPGDCLScript([]string{"db1"}, "u", map[string]string{}, []string{"r"})
	if strings.Contains(script, "GRANT CONNECT") {
		t.Fatalf("no owner should skip grants: %s", script)
	}
}

func TestRenderMySQLDCLScript(t *testing.T) {
	got := renderMySQLDCLScript([]string{"db1", "db2"}, "app_user", []string{"r", "c"})
	if !strings.Contains(got, "GRANT SELECT, INSERT ON `db1`.* TO 'app_user'@'%'") {
		t.Fatalf("unexpected: %s", got)
	}
}

func TestEncodeParseDCLExtra(t *testing.T) {
	raw := encodeDCLExtra(&dclExtra{
		Username: "sales_user", Permissions: []string{"r", "c"}, Databases: []string{"db_sales"},
		DBType: "postgres",
	})
	ex := parseDCLExtra(&store.Issue{NeedRollback: raw})
	if ex == nil || ex.Username != "sales_user" || len(ex.Databases) != 1 {
		t.Fatalf("parse failed: %#v raw=%s", ex, raw)
	}
}

func TestCreateUserSQL_PGEncryptedPassword_5x(t *testing.T) {
	for i := 1; i <= 5; i++ {
		user := fmt.Sprintf("dcl_new_user_%d", i)
		pwd := fmt.Sprintf("Aa1Pwd%02dXXXXXX", i)
		got := buildCreateUserSQL("postgres", user, pwd)
		want := fmt.Sprintf(`CREATE USER "%s" ENCRYPTED PASSWORD '%s'`, user, pwd)
		if got != want {
			t.Fatalf("[%d] got %q want %q", i, got, want)
		}
	}
}

func TestDCLGrantScript_CreateThenSingleDB_5x(t *testing.T) {
	for i := 1; i <= 5; i++ {
		user := fmt.Sprintf("grant_user_%d", i)
		db := fmt.Sprintf("db_single_%d", i)
		owner := fmt.Sprintf("owner_%d", i)
		create := buildCreateUserSQL("postgres", user, "Aa1PasswordXXXX")
		connect := buildPGDCLConnectSQL(db, user)
		stmts := buildPGDCLInDBStatements(user, owner, []string{"r", "c", "u", "d"})
		joined := strings.Join(append([]string{create, connect}, stmts...), ";\n")
		if strings.Contains(joined, "USAGE, CREATE") {
			t.Fatalf("[%d] schema CREATE forbidden: %s", i, joined)
		}
		if !strings.Contains(joined, `FOR ROLE "`+owner+`"`) {
			t.Fatalf("[%d] missing FOR ROLE: %s", i, joined)
		}
		script := renderPGDCLScript([]string{db}, user, map[string]string{db: owner}, []string{"r"})
		if strings.Count(script, "ALTER DEFAULT PRIVILEGES FOR ROLE") != 2 {
			t.Fatalf("[%d] want 2 executable ALTER: %s", i, script)
		}
	}
}

func TestDCLGrantScript_CreateThenMultiDB_5x(t *testing.T) {
	for i := 1; i <= 5; i++ {
		user := fmt.Sprintf("multi_user_%d", i)
		dbs := []string{fmt.Sprintf("db_a_%d", i), fmt.Sprintf("db_b_%d", i), fmt.Sprintf("db_c_%d", i)}
		owners := map[string]string{}
		for _, db := range dbs {
			owners[db] = "own_" + db
		}
		script := renderPGDCLScript(dbs, user, owners, []string{"r", "d"})
		if strings.Count(script, "GRANT CONNECT") != 3 {
			t.Fatalf("[%d] want 3 CONNECT: %s", i, script)
		}
		if strings.Count(script, "ALTER DEFAULT PRIVILEGES FOR ROLE") != 6 {
			t.Fatalf("[%d] want 6 ALTER: %s", i, script)
		}
		if strings.Contains(script, "USAGE, CREATE") {
			t.Fatalf("[%d] schema CREATE forbidden: %s", i, script)
		}
		for _, db := range dbs {
			if !strings.Contains(script, `FOR ROLE "own_`+db+`"`) {
				t.Fatalf("[%d] missing owner for %s: %s", i, db, script)
			}
		}
	}
}

func TestDCLExistingUserGrant_SingleAndMulti_5x(t *testing.T) {
	for i := 1; i <= 5; i++ {
		user := fmt.Sprintf("exist_user_%d", i)
		db1 := fmt.Sprintf("only_%d", i)
		stmts := buildPGDCLInDBStatements(user, "own_"+db1, []string{"r"})
		if strings.Contains(strings.Join(stmts, "\n"), "CREATE ON SCHEMA") {
			t.Fatalf("[%d] no schema CREATE: %#v", i, stmts)
		}
		dbs := []string{fmt.Sprintf("m1_%d", i), fmt.Sprintf("m2_%d", i)}
		owners := map[string]string{dbs[0]: "o1", dbs[1]: "o2"}
		script := renderPGDCLScript(dbs, user, owners, []string{"r", "c"})
		for _, line := range strings.Split(script, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "-- ALTER") {
				t.Fatalf("[%d] ALTER commented: %s", i, line)
			}
		}
	}
}

func TestDCLNotifyItem_NewVsReuse_5x(t *testing.T) {
	for i := 1; i <= 5; i++ {
		inst := &store.Instance{Host: "10.0.0.1", Port: 5432, Name: "pg"}
		user := fmt.Sprintf("u%d_user", i)
		dbs := []string{fmt.Sprintf("db%d", i)}
		created := buildDCLNotifyItem(inst, "", user, "SecretPwd1A", true, dbs, []string{"r", "c"})
		if !created.Created || created.Password == "" {
			t.Fatalf("[%d] created item bad: %+v", i, created)
		}
		reuse := buildDCLNotifyItem(inst, "pg.example.com", user, "", false, dbs, []string{"r"})
		if reuse.Created || reuse.Address != "pg.example.com:5432" {
			t.Fatalf("[%d] reuse item bad: %+v", i, reuse)
		}
		_ = notify.DCLNotifyItem(created)
	}
}

func TestPlanFeatureChecklist_NoSchemaCreateExecutableAlter(t *testing.T) {
	stmts := buildPGDCLInDBStatements("u", "real_owner", []string{"r"})
	if stmts[1] != `GRANT USAGE ON SCHEMA public TO "u"` {
		t.Fatalf("USAGE only: %s", stmts[1])
	}
	if !strings.HasPrefix(stmts[2], "ALTER DEFAULT PRIVILEGES FOR ROLE") {
		t.Fatal("ALTER must be executable stmt")
	}
	script := renderPGDCLScript([]string{"a"}, "u", map[string]string{"a": "own"}, []string{"r"})
	if strings.Contains(script, "-- ALTER DEFAULT") {
		t.Fatal("no commented ALTER")
	}
	if strings.Contains(script, "USAGE, CREATE") {
		t.Fatal("no schema CREATE")
	}
}
