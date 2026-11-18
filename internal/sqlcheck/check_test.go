package sqlcheck

import (
	"fmt"
	"strings"
	"testing"
)

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

func TestPostgresDDLAllowsTxnWrapper(t *testing.T) {
	sql := `
begin;
set local lock_timeout = '5s';
set local statement_timeout = '60s';
alter table public.tbl_finance_order_item add column if not exists purchase_quantity numeric(38, 8);
commit;`
	res := CheckTicketWithRules(sql, "ddl", "postgres", nil)
	for _, row := range res.Statements {
		if strings.Contains(row.Message, "语句必须是 DDL") {
			t.Fatalf("txn wrapper rejected: %+v", row)
		}
	}
	bare := CheckTicketWithRules("set local lock_timeout = '5s'; alter table t add column a int;", "ddl", "postgres", nil)
	if bare.OK || !hasMsg(bare, "SET LOCAL 必须写在 BEGIN 和 COMMIT 之间") {
		t.Fatalf("set local outside txn: %+v", bare)
	}
	open := CheckTicketWithRules("begin; set local statement_timeout = '60s'; alter table t add column a int;", "ddl", "postgres", nil)
	if open.OK || !hasMsg(open, "SET LOCAL 之后缺少 COMMIT") {
		t.Fatalf("missing commit: %+v", open)
	}
	other := CheckTicketWithRules("begin; select 1; commit;", "ddl", "postgres", nil)
	if other.OK || !hasMsg(other, "语句必须是 DDL") {
		t.Fatalf("select should stay rejected: %+v", other)
	}
	mysql := CheckTicketWithRules("begin; alter table t add column a int; commit;", "ddl", "mysql", nil)
	if mysql.OK || !hasMsg(mysql, "语句必须是 DDL") {
		t.Fatalf("mysql begin should stay rejected: %+v", mysql)
	}
}

func TestPostgresDDLRepairScript(t *testing.T) {
	sql := `
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '20s';
SET LOCAL search_path = pg_catalog, public;
DO $repair$
DECLARE
    parser_oid oid;
    config_oid oid;
    expected_aliases text[] := ARRAY['a', 'e', 'f', 'i', 'j', 'l', 'n', 's', 't', 'v', 'x', 'z'];
    actual_aliases text[];
    validation_text text := '普通菜品 销售汇总表';
BEGIN
    IF current_database() <> 'db_boh_agent' OR pg_is_in_recovery() THEN
        RAISE EXCEPTION 'Run this repair on the legacy BO TEST db_boh_agent primary';
    END IF;
    SELECT p.oid INTO parser_oid FROM pg_ts_parser AS p WHERE p.prsname = 'zhparser';
    IF parser_oid IS NULL THEN
        RAISE EXCEPTION 'The zhparser extension must already provide public.zhparser';
    END IF;
    SELECT c.oid INTO config_oid FROM pg_ts_config AS c WHERE c.cfgname = 'zh';
    IF config_oid IS NULL THEN
        CREATE TEXT SEARCH CONFIGURATION public.zh (PARSER = public.zhparser);
        ALTER TEXT SEARCH CONFIGURATION public.zh
            ADD MAPPING FOR n, v, a, i, e, l, j, f, s, t, z, x WITH pg_catalog.simple;
        COMMENT ON TEXT SEARCH CONFIGURATION public.zh
            IS 'Chinese full-text search configuration for BOH Agent knowledge chunks.';
    END IF;
    IF NOT (to_tsvector(config_oid::regconfig, validation_text)
            @@ plainto_tsquery(config_oid::regconfig, validation_text)) THEN
        RAISE EXCEPTION 'Existing public.zh uses another parser; review it before making changes';
    END IF;
END
$repair$;
COMMIT;`
	parts := SplitSQL(sql)
	if len(parts) != 6 {
		t.Fatalf("split = %d, want BEGIN, 3 SET, DO, COMMIT: %#v", len(parts), parts)
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(parts[4])), "DO ") {
		t.Fatalf("do statement = %s", parts[4])
	}
	res := CheckTicketWithRules(sql, "ddl", "postgres", nil)
	if !res.OK || hasMsg(res, "语句必须是 DDL") {
		t.Fatalf("repair script rejected: ok=%v %+v", res.OK, res.Statements)
	}
	bare := CheckTicketWithRules("set local search_path = public; create text search configuration public.zh (parser = public.zhparser);", "ddl", "postgres", nil)
	if bare.OK || !hasMsg(bare, "SET LOCAL 必须写在 BEGIN 和 COMMIT 之间") {
		t.Fatalf("search_path outside txn: %+v", bare)
	}
	ins := CheckTicketWithRules("begin; DO $$ BEGIN INSERT INTO t VALUES (1); END $$; commit;", "ddl", "postgres", nil)
	if ins.OK || !hasMsg(ins, "语句必须是 DDL") {
		t.Fatalf("insert inside do should stay rejected: %+v", ins)
	}
	mysqlDo := CheckTicketWithRules("DO $$ BEGIN CREATE TABLE t(a int); END $$;", "ddl", "mysql", nil)
	if mysqlDo.OK || !hasMsg(mysqlDo, "语句必须是 DDL") {
		t.Fatalf("mysql do should stay rejected: %+v", mysqlDo)
	}
}

func hasMsg(res Result, msg string) bool {
	for _, row := range res.Statements {
		if strings.Contains(row.Message, msg) {
			return true
		}
	}
	for _, item := range res.Errors {
		if strings.Contains(fmt.Sprint(item["message"]), msg) {
			return true
		}
	}
	return false
}

func TestParseCreateDBNames(t *testing.T) {
	names := ParseCreateDBNames("CREATE DATABASE `foo`; CREATE DATABASE bar")
	if len(names) != 2 || names[0] != "foo" || names[1] != "bar" {
		t.Fatalf("names=%v", names)
	}
}
