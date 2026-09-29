package handler

import "testing"

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
