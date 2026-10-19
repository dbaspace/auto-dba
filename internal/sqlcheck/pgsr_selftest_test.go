package sqlcheck

import (
	"strings"
	"testing"
)

// 与当前全局参数 ticket_review_rules 对齐的 PG/SR 工单自测。
func configuredPGRules() []Rule {
	return []Rule{
		{Name: "DDLCheckColumnComment", Value: "true"},
		{Name: "DDLCheckTableComment", Value: "true"},
		{Name: "DDLCheckColumnNullable", Value: "true"},
		{Name: "DDLCheckColumnDefault", Value: "true"},
		{Name: "DDLCheckPrimaryKeyExists", Value: "true"},
		{Name: "DDLForbidCreateDatabase", Value: "false"},
		{Name: "DDLForbidDropDatabase", Value: "false"},
		{Name: "DDLForbidDropTable", Value: `{"mode":"forbid","suffix":"_del"}`},
		{Name: "DDLIndexNameSpec", Value: "false"},
		{Name: "DDLRequireUpdateTime", Value: `{"enabled":false}`},
		{Name: "DDLForbidTruncate", Value: "false"},
		{Name: "DDLForbidDropSchema", Value: "false"},
		{Name: "DDLMaxColumnCount", Value: "64"},
		{Name: "DDLTableNamePrefix", Value: "tbl_"},
		{Name: "DDLDatabaseNamePrefix", Value: "db_"},
		{Name: "DDLCharVarcharLength", Value: `{"max_char":50,"max_varchar":5000,"unlimited":false}`},
		{Name: "DDLColumnTypeDisallow", Value: "float,double,double precision,real"},
		{Name: "DDLAllowDropColumn", Value: "true"},
		{Name: "DDLColumnTypeChange", Value: "true"},
		{Name: "DDLDisallowAddColumnWithDefault", Value: "true"},
		{Name: "DDLDisallowAddNotNull", Value: "true"},
		{Name: "DDLAddCheckNotValid", Value: "true"},
		{Name: "DDLAddFKNotValid", Value: "true"},
		{Name: "DDLIndexCreateConcurrently", Value: "500000"},
		{Name: "DDLAddColumnNotNullRequireDefault", Value: "true"},
		{Name: "DDLForbidCluster", Value: "true"},
		{Name: "DDLForbidGrant", Value: "true"},
		{Name: "DDLForbidLockVacuumFull", Value: "true"},
		{Name: "DDLBackwardIncompatible", Value: "true"},
		{Name: "DMLWhere", Value: "1=1,true"},
		{Name: "DMLForbidTautology", Value: "true"},
		{Name: "DMLInsertMustSpecifyColumn", Value: "true"},
		{Name: "DMLForbidDDL", Value: "true"},
		{Name: "DMLUpdateSetAnd", Value: "true"},
		{Name: "DMLAllowSubquery", Value: "false"},
		{Name: "DMLAllowMultiTable", Value: "false"},
		{Name: "DMLInsertRowLimit", Value: "1000"},
		{Name: "DMLInsertDisallowOrderByRand", Value: "true"},
		{Name: "DMLInsertDisallowSelectStar", Value: "true"},
	}
}

func configuredSRRules() []Rule {
	return []Rule{
		{Name: "DDLCheckColumnComment", Value: "true"},
		{Name: "DDLCheckTableComment", Value: "true"},
		{Name: "DDLCheckColumnNullable", Value: "true"},
		{Name: "DDLCheckColumnDefault", Value: "true"},
		{Name: "DDLEnablePrimaryKey", Value: "true"},
		{Name: "DDLForbidCreateDatabase", Value: "false"},
		{Name: "DDLForbidDropDatabase", Value: "false"},
		{Name: "DDLForbidDropTable", Value: `{"mode":"forbid","suffix":"_del"}`},
		{Name: "DDLForbidTruncate", Value: "false"},
		{Name: "DDLColumnTypeDisallow", Value: "float,double,double precision,real"},
		{Name: "DDLAllowDropColumn", Value: "true"},
		{Name: "DDLColumnTypeChange", Value: "true"},
		{Name: "DDLForbidGrant", Value: "true"},
		{Name: "DDLAddColumnRequireDefault", Value: "true"},
		{Name: "DMLWhere", Value: "1=1,true"},
		{Name: "DMLForbidTautology", Value: "true"},
		{Name: "DMLInsertMustSpecifyColumn", Value: "true"},
		{Name: "DMLForbidDDL", Value: "true"},
		{Name: "DMLAllowSubquery", Value: "false"},
		{Name: "DMLInsertDisallowOrderByRand", Value: "true"},
		{Name: "DMLInsertDisallowSelectStar", Value: "true"},
	}
}

func hasRuleID(res Result, id string) bool {
	for _, st := range res.Statements {
		if strings.EqualFold(st.RuleID, id) {
			return true
		}
	}
	for _, e := range res.Errors {
		if rid, _ := e["rule_id"].(string); strings.EqualFold(rid, id) {
			return true
		}
	}
	for _, e := range res.Warns {
		if rid, _ := e["rule_id"].(string); strings.EqualFold(rid, id) {
			return true
		}
	}
	return false
}

func TestCheckCreateDBWithRules_prefix(t *testing.T) {
	rules := []Rule{{Name: "DDLDatabaseNamePrefix", Value: `{"prefix":"db_"}`}}
	res := CheckCreateDBWithRules("CREATE DATABASE foo;", "postgres", rules)
	if res.OK || !hasRuleID(res, "DDLDatabaseNamePrefix") {
		t.Fatalf("want prefix error, got %+v", res)
	}
	res = CheckCreateDBWithRules("CREATE DATABASE db_app;", "postgres", rules)
	if !res.OK {
		t.Fatalf("db_ prefix should pass: %+v", res)
	}
}

func TestPGSRTicketSelftest(t *testing.T) {
	type tc struct {
		name, db, typ, sql string
		wantOK             bool
		errRule, warnRule  string
		rules              []Rule
	}
	pg, sr := configuredPGRules(), configuredSRRules()
	allowCreate := append([]Rule{}, pg...)
	for i := range allowCreate {
		if allowCreate[i].Name == "DDLForbidCreateDatabase" {
			allowCreate[i].Value = "true"
		}
	}
	rowLimit := append([]Rule{}, pg...)
	for i := range rowLimit {
		if rowLimit[i].Name == "DMLInsertRowLimit" {
			rowLimit[i].Value = "2"
		}
	}
	idxNameOn := append([]Rule{}, pg...)
	for i := range idxNameOn {
		if idxNameOn[i].Name == "DDLIndexNameSpec" {
			idxNameOn[i].Value = "true"
		}
	}
	needUpdateTime := append([]Rule{}, pg...)
	for i := range needUpdateTime {
		if needUpdateTime[i].Name == "DDLRequireUpdateTime" {
			needUpdateTime[i].Value = `{"enabled":true,"column":"update_time"}`
		}
	}

	cases := []tc{
		{name: "pg_dml_tautology", db: "postgres", typ: "dml", sql: "UPDATE t SET a=1 WHERE 1=1;", errRule: "DMLForbidTautology", rules: pg},
		{name: "pg_dml_no_where", db: "postgres", typ: "dml", sql: "DELETE FROM t;", errRule: "DMLWhere", rules: pg},
		{name: "pg_dml_insert_no_cols", db: "postgres", typ: "dml", sql: "INSERT INTO t VALUES (1);", errRule: "DMLInsertMustSpecifyColumn", rules: pg},
		{name: "pg_dml_forbid_ddl", db: "postgres", typ: "dml", sql: "CREATE TABLE t (id int);", errRule: "DMLForbidDDL", rules: pg},
		{name: "pg_dml_subquery_warn", db: "postgres", typ: "dml", sql: "UPDATE t SET a=(SELECT 1) WHERE id=1;", wantOK: true, warnRule: "DMLAllowSubquery", rules: pg},
		{name: "pg_dml_insert_row_limit", db: "postgres", typ: "dml", sql: "INSERT INTO t (id) VALUES (1),(2),(3);", errRule: "DMLInsertRowLimit", rules: rowLimit},
		{name: "pg_dml_ok", db: "postgres", typ: "dml", sql: "UPDATE t SET a=1 WHERE id=1;", wantOK: true, rules: pg},
		{name: "pg_dml_set_and", db: "postgres", typ: "dml", sql: "update t set a=1 and b=2 where id>0;", errRule: "DMLUpdateSetAnd", rules: pg},
		{name: "pg_dml_multi_table_warn", db: "postgres", typ: "dml", sql: "UPDATE t SET a=1 FROM s WHERE t.id=s.id;", wantOK: true, warnRule: "DMLAllowMultiTable", rules: pg},
		{name: "pg_ddl_truncate", db: "postgres", typ: "ddl", sql: "TRUNCATE t;", errRule: "DDLForbidTruncate", rules: pg},
		{name: "pg_ddl_drop_schema", db: "postgres", typ: "ddl", sql: "DROP SCHEMA public;", errRule: "DDLForbidDropSchema", rules: pg},
		{name: "pg_ddl_drop_table", db: "postgres", typ: "ddl", sql: "DROP TABLE t;", errRule: "DDLForbidDropTable", rules: pg},
		{name: "pg_ddl_create_db", db: "postgres", typ: "ddl", sql: "CREATE DATABASE foo;", errRule: "DDLForbidCreateDatabase", rules: pg},
		{name: "pg_ddl_create_db_allow_prefix", db: "postgres", typ: "ddl", sql: "CREATE DATABASE foo;", errRule: "DDLDatabaseNamePrefix", rules: allowCreate},
		{name: "pg_ddl_create_db_allow_ok", db: "postgres", typ: "ddl", sql: "CREATE DATABASE db_foo;", wantOK: true, rules: allowCreate},
		{name: "pg_ddl_add_col_volatile_default", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ADD COLUMN ts timestamptz NOT NULL DEFAULT now();", errRule: "DDLDisallowAddColumnWithDefault", rules: pg},
		{name: "pg_ddl_grant", db: "postgres", typ: "ddl", sql: "GRANT SELECT ON t TO u;", errRule: "DDLForbidGrant", rules: pg},
		{name: "pg_ddl_lock", db: "postgres", typ: "ddl", sql: "LOCK TABLE t;", errRule: "DDLForbidLockVacuumFull", rules: pg},
		{name: "pg_ddl_vacuum_full", db: "postgres", typ: "ddl", sql: "VACUUM FULL t;", errRule: "DDLForbidLockVacuumFull", rules: pg},
		{name: "pg_dml_grant", db: "postgres", typ: "dml", sql: "GRANT SELECT ON t TO u;", errRule: "DDLForbidGrant", rules: pg},
		{name: "pg_ddl_add_col_const_default", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ADD COLUMN name varchar(20) NOT NULL DEFAULT 'x'; COMMENT ON COLUMN t.name IS 'n';", wantOK: true, rules: pg},
		{name: "pg_ddl_add_omit_column", db: "postgres", typ: "ddl", sql: "ALTER TABLE public.tbl_report_dim ADD additional jsonb NOT NULL DEFAULT '{}'; COMMENT ON COLUMN tbl_report_dim.additional IS 'extra';", wantOK: true, rules: pg},
		{name: "pg_ddl_add_col_missing_comment", db: "postgres", typ: "ddl", sql: "ALTER TABLE tbl_test ADD COLUMN xx int NOT NULL DEFAULT '0';", errRule: "DDLCheckColumnComment", rules: pg},
		{name: "pg_ddl_add_col_empty_comment", db: "postgres", typ: "ddl", sql: "ALTER TABLE tbl_test ADD COLUMN xx int NOT NULL DEFAULT '0'; COMMENT ON COLUMN tbl_test.xx IS '';", errRule: "DDLCheckColumnComment", rules: pg},
		{name: "pg_ddl_select_not_syntax", db: "postgres", typ: "ddl", sql: "select 1;", wantOK: false},
		{name: "pg_ddl_set_not_null", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ALTER COLUMN name SET NOT NULL;", errRule: "DDLDisallowAddNotNull", rules: pg},
		{name: "pg_ddl_check_not_valid", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ADD CHECK (name <> '');", errRule: "DDLAddCheckNotValid", rules: pg},
		{name: "pg_ddl_float", db: "postgres", typ: "ddl", sql: "CREATE TABLE tbl_t (id bigserial PRIMARY KEY, x float NOT NULL DEFAULT 0);", errRule: "DDLColumnTypeDisallow", rules: pg},
		{name: "pg_ddl_varchar_len", db: "postgres", typ: "ddl", sql: "CREATE TABLE tbl_t (id bigserial PRIMARY KEY, name varchar(9000) NOT NULL DEFAULT '');", errRule: "DDLCharVarcharLength", rules: pg},
		{name: "pg_ddl_table_prefix", db: "postgres", typ: "ddl", sql: "CREATE TABLE t (id bigserial PRIMARY KEY);", errRule: "DDLTableNamePrefix", rules: pg},
		{name: "pg_ddl_index_name_warn", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ADD CONSTRAINT unique_name UNIQUE (name);", wantOK: true, warnRule: "DDLIndexNameSpec", rules: idxNameOn},
		{name: "pg_ddl_index_concurrent_warn", db: "postgres", typ: "ddl", sql: "CREATE INDEX idx_t_name ON t (name);", wantOK: true, warnRule: "DDLIndexCreateConcurrently", rules: pg},
		{name: "pg_ddl_require_update_time", db: "postgres", typ: "ddl", sql: "CREATE TABLE tbl_t (id bigserial PRIMARY KEY);", errRule: "DDLRequireUpdateTime", rules: needUpdateTime},
		{name: "pg_ddl_drop_col_warn", db: "postgres", typ: "ddl", sql: "ALTER TABLE t DROP COLUMN name;", wantOK: true, warnRule: "DDLBackwardIncompatible", rules: pg},
		{name: "pg_ddl_alter_type_warn", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ALTER COLUMN name TYPE integer;", wantOK: true, warnRule: "DDLColumnTypeChange", rules: pg},
		{name: "pg_ddl_ctas_comment", db: "postgres", typ: "ddl", sql: "CREATE TABLE tbl_t2 AS SELECT * FROM t1;", errRule: "DDLCheckTableComment", rules: pg},
		{name: "sr_dml_no_where", db: "starrocks", typ: "dml", sql: "DELETE FROM t;", errRule: "DMLWhere", rules: sr},
		{name: "sr_dml_tautology", db: "starrocks", typ: "dml", sql: "UPDATE t SET a=1 WHERE 1=1;", errRule: "DMLForbidTautology", rules: sr},
		{name: "sr_dml_insert_no_cols", db: "starrocks", typ: "dml", sql: "INSERT INTO t VALUES (1);", errRule: "DMLInsertMustSpecifyColumn", rules: sr},
		{name: "sr_dml_forbid_ddl", db: "starrocks", typ: "dml", sql: "CREATE TABLE t (id int);", errRule: "DMLForbidDDL", rules: sr},
		{name: "sr_dml_ok", db: "starrocks", typ: "dml", sql: "UPDATE t SET a=1 WHERE id=1;", wantOK: true, rules: sr},
		{name: "sr_ddl_truncate", db: "starrocks", typ: "ddl", sql: "TRUNCATE TABLE t;", errRule: "DDLForbidTruncate", rules: sr},
		{name: "sr_ddl_create_db", db: "starrocks", typ: "ddl", sql: "CREATE DATABASE foo;", errRule: "DDLForbidCreateDatabase", rules: sr},
		{name: "sr_ddl_grant", db: "starrocks", typ: "ddl", sql: "GRANT SELECT ON t TO u;", errRule: "DDLForbidGrant", rules: sr},
		{name: "sr_ddl_add_col_no_default", db: "starrocks", typ: "ddl", sql: "ALTER TABLE t ADD COLUMN name varchar(20);", errRule: "DDLAddColumnRequireDefault", rules: sr},
		{name: "sr_ddl_float", db: "starrocks", typ: "ddl", sql: `ALTER TABLE t ADD COLUMN x float NOT NULL DEFAULT "1";`, errRule: "DDLColumnTypeDisallow", rules: sr},
		{name: "sr_ddl_no_key", db: "starrocks", typ: "ddl", sql: `CREATE TABLE t (id int NOT NULL DEFAULT "0", name varchar(20) NOT NULL DEFAULT "");`, errRule: "DDLEnablePrimaryKey", rules: sr},
		{name: "sr_ddl_modify_type_warn", db: "starrocks", typ: "ddl", sql: `ALTER TABLE t MODIFY COLUMN name INT NOT NULL DEFAULT "0"`, wantOK: true, warnRule: "DDLColumnTypeChange", rules: sr},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			res := CheckTicketWithRules(c.sql, c.typ, c.db, c.rules)
			if res.OK != c.wantOK {
				t.Fatalf("ok=%v want %v stmts=%+v", res.OK, c.wantOK, res.Statements)
			}
			if c.errRule != "" && !hasRuleID(res, c.errRule) {
				t.Fatalf("errors 应含 %s: %+v", c.errRule, res.Statements)
			}
			if c.warnRule != "" && !hasRuleID(res, c.warnRule) {
				t.Fatalf("warns 应含 %s: %+v", c.warnRule, res.Statements)
			}
		})
	}
}
