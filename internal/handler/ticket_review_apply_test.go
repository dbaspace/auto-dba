package handler

import (
	"strings"
	"testing"

	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/store"
)

func catalogRules(engine string) []sqlcheck.Rule {
	return toCheckRules(store.DefaultTicketReviewRules(engine))
}

func applyTicketReview(sqlContent, issueType, dbType string, rules []sqlcheck.Rule) sqlcheck.Result {
	engine := store.NormalizeReviewEngine(dbType)
	return sqlcheck.CheckTicketWithRules(sqlContent, issueType, engine, rules)
}

func resultHasRule(res sqlcheck.Result, id string) bool {
	for _, st := range res.Statements {
		if strings.EqualFold(st.RuleID, id) {
			return true
		}
	}
	for _, group := range [][]map[string]any{res.Errors, res.Warns} {
		for _, e := range group {
			if rid, _ := e["rule_id"].(string); strings.EqualFold(rid, id) {
				return true
			}
		}
	}
	return false
}

func TestResolveReviewEngineAliases(t *testing.T) {
	h := &TicketHandler{}
	cases := []struct{ in, want string }{
		{"postgres", "postgres"},
		{"postgresql", "postgres"},
		{"pg", "postgres"},
		{"PostgreSQL", "postgres"},
		{"starrocks", "starrocks"},
		{"sr", "starrocks"},
		{"SR", "starrocks"},
	}
	for _, c := range cases {
		if got := h.resolveReviewEngine(c.in, ""); got != c.want {
			t.Fatalf("db_type=%q => %q want %q", c.in, got, c.want)
		}
	}
}

// TestTicketDDLDMLAppliesCatalogRules 用当前「审核规则」目录默认值（JSON 版）
// 验证 DDL/DML 工单检测会命中对应策略；同一用例覆盖 pg/postgresql/pg 与 sr/starrocks 别名。
func TestTicketDDLDMLAppliesCatalogRules(t *testing.T) {
	type tc struct {
		name, db, typ, sql, errRule string
		wantOK                      bool
		aliases                     []string
	}
	pg, sr := catalogRules("postgres"), catalogRules("starrocks")
	if len(pg) == 0 || len(sr) == 0 {
		t.Fatalf("catalog empty pg=%d sr=%d", len(pg), len(sr))
	}

	cases := []tc{
		{name: "pg_dml_no_where", db: "postgres", typ: "dml", sql: "DELETE FROM t;", errRule: "DMLWhere", aliases: []string{"pg", "postgresql"}},
		{name: "pg_dml_tautology", db: "postgres", typ: "dml", sql: "UPDATE t SET a=1 WHERE 1=1;", errRule: "DMLForbidTautology"},
		{name: "pg_dml_insert_cols", db: "postgres", typ: "dml", sql: "INSERT INTO t VALUES (1);", errRule: "DMLInsertMustSpecifyColumn"},
		{name: "pg_dml_forbid_ddl", db: "postgres", typ: "dml", sql: "CREATE TABLE t (id int);", errRule: "DMLForbidDDL"},
		{name: "pg_dml_ok", db: "postgres", typ: "dml", sql: "UPDATE t SET a=1 WHERE id=1;", wantOK: true},
		{name: "pg_ddl_truncate", db: "postgres", typ: "ddl", sql: "TRUNCATE t;", errRule: "DDLForbidTruncate"},
		{name: "pg_ddl_grant", db: "postgres", typ: "ddl", sql: "GRANT SELECT ON t TO u;", errRule: "DDLForbidGrant"},
		{name: "pg_ddl_lock", db: "postgres", typ: "ddl", sql: "LOCK TABLE t;", errRule: "DDLForbidLockVacuumFull"},
		{name: "pg_ddl_vacuum_full", db: "postgres", typ: "ddl", sql: "VACUUM FULL t;", errRule: "DDLForbidLockVacuumFull"},
		{name: "pg_ddl_drop_schema", db: "postgres", typ: "ddl", sql: "DROP SCHEMA public;", errRule: "DDLForbidDropSchema"},
		{name: "pg_ddl_volatile_default", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ADD COLUMN ts timestamptz NOT NULL DEFAULT now();", errRule: "DDLDisallowAddColumnWithDefault"},
		{name: "pg_ddl_table_prefix", db: "postgres", typ: "ddl", sql: "CREATE TABLE t (id bigserial PRIMARY KEY, update_time timestamptz NOT NULL DEFAULT now());", errRule: "DDLTableNamePrefix"},
		{name: "pg_ddl_add_ok", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ADD COLUMN name varchar(20) NOT NULL DEFAULT 'x'; COMMENT ON COLUMN t.name IS 'n';", wantOK: true},
		{name: "sr_dml_no_where", db: "starrocks", typ: "dml", sql: "DELETE FROM t;", errRule: "DMLWhere", aliases: []string{"sr"}},
		{name: "sr_dml_tautology", db: "starrocks", typ: "dml", sql: "UPDATE t SET a=1 WHERE 1=1;", errRule: "DMLForbidTautology"},
		{name: "sr_dml_insert_cols", db: "starrocks", typ: "dml", sql: "INSERT INTO t VALUES (1);", errRule: "DMLInsertMustSpecifyColumn"},
		{name: "sr_dml_forbid_ddl", db: "starrocks", typ: "dml", sql: "CREATE TABLE t (id int);", errRule: "DMLForbidDDL"},
		{name: "sr_dml_ok", db: "starrocks", typ: "dml", sql: "UPDATE t SET a=1 WHERE id=1;", wantOK: true},
		{name: "sr_ddl_truncate", db: "starrocks", typ: "ddl", sql: "TRUNCATE TABLE t;", errRule: "DDLForbidTruncate"},
		{name: "sr_ddl_grant", db: "starrocks", typ: "ddl", sql: "GRANT SELECT ON t TO u;", errRule: "DDLForbidGrant"},
		{name: "sr_ddl_add_no_default", db: "starrocks", typ: "ddl", sql: "ALTER TABLE t ADD COLUMN name varchar(20);", errRule: "DDLAddColumnRequireDefault"},
		{name: "sr_ddl_no_key", db: "starrocks", typ: "ddl", sql: `CREATE TABLE t (id int NOT NULL DEFAULT "0", name varchar(20) NOT NULL DEFAULT "");`, errRule: "DDLEnablePrimaryKey"},
	}

	for _, c := range cases {
		c := c
		engines := append([]string{c.db}, c.aliases...)
		rules := catalogRules(c.db)
		for _, engine := range engines {
			engine := engine
			t.Run(c.name+"/"+engine, func(t *testing.T) {
				res := applyTicketReview(c.sql, c.typ, engine, rules)
				if res.OK != c.wantOK {
					t.Fatalf("ok=%v want %v stmts=%+v", res.OK, c.wantOK, res.Statements)
				}
				if c.errRule != "" && !resultHasRule(res, c.errRule) {
					t.Fatalf("应命中 %s: %+v", c.errRule, res.Statements)
				}
				if !c.wantOK {
					gate := reviewGateResult(res)
					if gate.Success || gate.Message != "执行前审核规则未通过" {
						t.Fatalf("执行前 gate: %+v", gate)
					}
				}
			})
		}
	}
}

// TestTicketReviewRuleValueVersions 同一条 SQL 在旧版字符串取值与当前 JSON 目录取值下都应命中同一规则。
func TestTicketReviewRuleValueVersions(t *testing.T) {
	type ver struct {
		name  string
		rules []sqlcheck.Rule
	}
	versions := []ver{
		{"legacy", []sqlcheck.Rule{
			{Name: "DMLWhere", Value: "1=1,true"},
			{Name: "DMLForbidTautology", Value: "true"},
			{Name: "DMLInsertMustSpecifyColumn", Value: "true"},
			{Name: "DDLForbidTruncate", Value: "false"},
			{Name: "DDLColumnTypeDisallow", Value: "float,double,double precision,real"},
			{Name: "DDLTableNamePrefix", Value: "tbl_"},
			{Name: "DDLEnablePrimaryKey", Value: "true"},
			{Name: "DDLAddColumnRequireDefault", Value: "true"},
			{Name: "DDLForbidLockVacuumFull", Value: "true"},
		}},
		{"catalog_json", []sqlcheck.Rule{
			{Name: "DMLWhere", Value: `{"tautologies":["1=1","true"]}`},
			{Name: "DMLForbidTautology", Value: "true"},
			{Name: "DMLInsertMustSpecifyColumn", Value: "true"},
			{Name: "DDLForbidTruncate", Value: `{"allow":false}`},
			{Name: "DDLColumnTypeDisallow", Value: `{"disallow_types":["float","double","double precision","real"]}`},
			{Name: "DDLTableNamePrefix", Value: `{"prefix":"tbl_"}`},
			{Name: "DDLEnablePrimaryKey", Value: "true"},
			{Name: "DDLAddColumnRequireDefault", Value: "true"},
			{Name: "DDLForbidLockVacuumFull", Value: "true"},
		}},
	}
	cases := []struct {
		name, db, typ, sql, errRule string
	}{
		{"dml_where", "postgres", "dml", "DELETE FROM t;", "DMLWhere"},
		{"dml_tautology", "pg", "dml", "UPDATE t SET a=1 WHERE 1=1;", "DMLForbidTautology"},
		{"ddl_truncate", "postgresql", "ddl", "TRUNCATE t;", "DDLForbidTruncate"},
		{"ddl_lock", "postgres", "ddl", "LOCK TABLE t;", "DDLForbidLockVacuumFull"},
		{"ddl_float", "postgres", "ddl", "CREATE TABLE tbl_t (id int NOT NULL DEFAULT 0, x float NOT NULL DEFAULT 0);", "DDLColumnTypeDisallow"},
		{"ddl_prefix", "postgres", "ddl", "CREATE TABLE t (id int PRIMARY KEY);", "DDLTableNamePrefix"},
		{"sr_add_default", "sr", "ddl", "ALTER TABLE t ADD COLUMN name varchar(20);", "DDLAddColumnRequireDefault"},
		{"sr_key", "starrocks", "ddl", `CREATE TABLE t (id int NOT NULL DEFAULT "0");`, "DDLEnablePrimaryKey"},
	}
	for _, v := range versions {
		for _, c := range cases {
			c, v := c, v
			t.Run(v.name+"/"+c.name, func(t *testing.T) {
				res := applyTicketReview(c.sql, c.typ, c.db, v.rules)
				if res.OK || !resultHasRule(res, c.errRule) {
					t.Fatalf("ok=%v 应命中 %s: %+v", res.OK, c.errRule, res.Statements)
				}
			})
		}
	}
}

func TestTicketReviewEngineIsolation(t *testing.T) {
	pg, sr := catalogRules("postgres"), catalogRules("starrocks")

	lock := applyTicketReview("LOCK TABLE t;", "ddl", "postgres", pg)
	if lock.OK || !resultHasRule(lock, "DDLForbidLockVacuumFull") {
		t.Fatalf("PG LOCK 应命中 DDLForbidLockVacuumFull: %+v", lock.Statements)
	}
	srLock := applyTicketReview("LOCK TABLE t;", "ddl", "starrocks", sr)
	if resultHasRule(srLock, "DDLForbidLockVacuumFull") {
		t.Fatalf("SR 不应套用 PG 专用 LOCK 规则: %+v", srLock.Statements)
	}

	create := `CREATE TABLE t (id int NOT NULL DEFAULT 0, name varchar(20) NOT NULL DEFAULT '');`
	pgCreate := applyTicketReview(create, "ddl", "pg", pg)
	srCreate := applyTicketReview(create, "ddl", "sr", sr)
	if resultHasRule(pgCreate, "DDLEnablePrimaryKey") {
		t.Fatalf("PG 不应命中 SR 的 DDLEnablePrimaryKey: %+v", pgCreate.Statements)
	}
	if !resultHasRule(srCreate, "DDLEnablePrimaryKey") {
		t.Fatalf("SR 建表应命中 DDLEnablePrimaryKey: %+v", srCreate.Statements)
	}
	if !resultHasRule(pgCreate, "DDLTableNamePrefix") && !resultHasRule(pgCreate, "DDLCheckPrimaryKeyExists") {
		t.Fatalf("PG 建表应命中前缀或主键规则: %+v", pgCreate.Statements)
	}
	if resultHasRule(srCreate, "DDLTableNamePrefix") {
		t.Fatalf("SR 不应套用 PG 表名前缀: %+v", srCreate.Statements)
	}

	add := "ALTER TABLE t ADD COLUMN name varchar(20);"
	pgAdd := applyTicketReview(add, "ddl", "postgres", pg)
	srAdd := applyTicketReview(add, "ddl", "starrocks", sr)
	if resultHasRule(pgAdd, "DDLAddColumnRequireDefault") {
		t.Fatalf("PG 不应命中 SR 加列 DEFAULT 规则: %+v", pgAdd.Statements)
	}
	if !resultHasRule(srAdd, "DDLAddColumnRequireDefault") {
		t.Fatalf("SR 加列应命中 DDLAddColumnRequireDefault: %+v", srAdd.Statements)
	}
}

func TestCheckTicketSQLUsesStoreReviewRules(t *testing.T) {
	db := testStore(t)
	h := &TicketHandler{Store: db}

	cases := []struct {
		name, db, typ, sql, errRule string
		wantOK                      bool
	}{
		{name: "pg_dml", db: "pg", typ: "dml", sql: "DELETE FROM t;", errRule: "DMLWhere"},
		{name: "pg_ddl", db: "postgresql", typ: "ddl", sql: "TRUNCATE t;", errRule: "DDLForbidTruncate"},
		{name: "sr_dml", db: "sr", typ: "dml", sql: "UPDATE t SET a=1 WHERE 1=1;", errRule: "DMLForbidTautology"},
		{name: "sr_ddl", db: "starrocks", typ: "ddl", sql: "TRUNCATE TABLE t;", errRule: "DDLForbidTruncate"},
		{name: "pg_dml_ok", db: "postgres", typ: "dml", sql: "UPDATE t SET a=1 WHERE id=1;", wantOK: true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			res := h.checkTicketSQL(c.sql, c.typ, c.db, "", "", false)
			if res.OK != c.wantOK {
				t.Fatalf("ok=%v want %v stmts=%+v", res.OK, c.wantOK, res.Statements)
			}
			if c.errRule != "" && !resultHasRule(res, c.errRule) {
				t.Fatalf("工单检测未套用审核规则 %s: %+v", c.errRule, res.Statements)
			}
		})
	}
}
