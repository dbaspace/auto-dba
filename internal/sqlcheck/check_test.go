package sqlcheck

import "testing"

func TestCheckCreateDB(t *testing.T) {
	ok := CheckCreateDB("CREATE DATABASE foo;")
	if !ok.OK {
		t.Fatalf("expected ok, got %+v", ok)
	}
	bad := CheckCreateDB("ALTER TABLE t ADD c int;")
	if bad.OK {
		t.Fatal("expected fail for non create database")
	}
}

func TestCheckTicketConstraints(t *testing.T) {
	ddl := CheckTicket("CREATE DATABASE x", "ddl", true, true)
	if ddl.OK {
		t.Fatal("create database should be rejected on ddl ticket")
	}
	dml := CheckTicket("DELETE FROM t", "dml", true, true)
	if dml.OK {
		t.Fatal("delete without where should fail")
	}
	dmlOK := CheckTicket("DELETE FROM t WHERE id=1", "dml", true, true)
	if !dmlOK.OK {
		t.Fatalf("expected ok: %+v", dmlOK)
	}
}

func TestCheckTicketWithEngineRules(t *testing.T) {
	mysql := CheckTicketWithRules("DELETE FROM t", "dml", "mysql", []Rule{
		{Name: "check_dml_where", Value: "true", Label: "检查 DML WHERE 条件"},
	})
	if mysql.OK {
		t.Fatal("mysql check_dml_where should reject delete without where")
	}
	pg := CheckTicketWithRules("TRUNCATE t", "ddl", "postgres", []Rule{
		{Name: "DDLForbidTruncate", Value: "false", Label: "允许 TRUNCATE"},
	})
	if pg.OK {
		t.Fatal("postgres DDLForbidTruncate allow=false should reject")
	}
	pgAllow := CheckTicketWithRules("TRUNCATE t", "ddl", "postgres", []Rule{
		{Name: "DDLForbidTruncate", Value: "true"},
	})
	if !pgAllow.OK {
		t.Fatalf("postgres allow truncate should pass: %+v", pgAllow)
	}
	sr := CheckTicketWithRules("DROP DATABASE foo", "ddl", "starrocks", []Rule{
		{Name: "DDLForbidDropDatabase", Value: "false"},
	})
	if sr.OK {
		t.Fatal("starrocks should forbid drop database")
	}
	mysqlIgnorePG := CheckTicketWithRules("TRUNCATE t", "ddl", "mysql", []Rule{
		{Name: "check_dml_where", Value: "true"},
	})
	if !mysqlIgnorePG.OK {
		t.Fatalf("mysql catalog without truncate rule should pass: %+v", mysqlIgnorePG)
	}
}

func TestParseCreateDBNames(t *testing.T) {
	names := ParseCreateDBNames("CREATE DATABASE `foo`; CREATE DATABASE bar")
	if len(names) != 2 || names[0] != "foo" || names[1] != "bar" {
		t.Fatalf("names=%v", names)
	}
}
