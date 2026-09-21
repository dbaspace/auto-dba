package sqlcheck

import "testing"

// 与「配置 → 审核规则」MySQL 默认参数对齐（goInception 未启用时的本地规则）。
func configuredMySQLRules() []Rule {
	return []Rule{
		{Name: "check_dml_where", Value: "true"},
		{Name: "check_dml_limit", Value: "false"},
		{Name: "check_dml_orderby", Value: "false"},
		{Name: "check_insert_field", Value: "false"},
		{Name: "max_update_rows", Value: "10000"},
		{Name: "enable_blob_type", Value: "true"},
		{Name: "enable_timestamp_type", Value: "true"},
		{Name: "enable_identifer_keyword", Value: "false"},
		{Name: "check_table_comment", Value: "false"},
		{Name: "check_column_comment", Value: "false"},
		{Name: "check_primary_key", Value: "false"},
		{Name: "enable_nullable", Value: "true"},
		{Name: "enable_drop_table", Value: "true"},
		{Name: "disable_drop_database", Value: "true"},
		{Name: "enable_set_engine", Value: "true"},
		{Name: "max_char_length", Value: "255"},
		{Name: "max_varchar_length", Value: "16383"},
		{Name: "max_key_parts", Value: "5"},
		{Name: "check_identifier", Value: "false"},
		{Name: "enable_select_star", Value: "true"},
		{Name: "check_index_prefix", Value: "false"},
		{Name: "enable_partition_table", Value: "true"},
	}
}

func mysqlInsertColsOn() []Rule {
	out := append([]Rule{}, configuredMySQLRules()...)
	for i := range out {
		if out[i].Name == "check_insert_field" {
			out[i].Value = "true"
		}
	}
	return out
}

func mysqlBlobOff() []Rule {
	out := append([]Rule{}, configuredMySQLRules()...)
	for i := range out {
		if out[i].Name == "enable_blob_type" {
			out[i].Value = "false"
		}
	}
	return out
}

// TestAllTicketTypesSelftest 覆盖 mysql / postgres / starrocks 的
// DDL、DML、DQL、DCL、建库 五类工单，对照审核规则配置。
func TestAllTicketTypesSelftest(t *testing.T) {
	type tc struct {
		name, db, typ, sql string
		wantOK             bool
		errRule            string
		rules              []Rule
		createdb           bool
	}
	mysql, pg, sr := configuredMySQLRules(), configuredPGRules(), configuredSRRules()

	cases := []tc{
		// ---- MySQL DDL ----
		{name: "mysql_ddl_drop_database", db: "mysql", typ: "ddl", sql: "DROP DATABASE foo;", errRule: "disable_drop_database", rules: mysql},
		{name: "mysql_ddl_drop_table_allow", db: "mysql", typ: "ddl", sql: "DROP TABLE t;", wantOK: true, rules: mysql},
		{name: "mysql_ddl_create_ok", db: "mysql", typ: "ddl", sql: "CREATE TABLE t (id int, name varchar(20));", wantOK: true, rules: mysql},
		{name: "mysql_ddl_blob_disallow", db: "mysql", typ: "ddl", sql: "ALTER TABLE t ADD COLUMN bin blob;", errRule: "enable_blob_type", rules: mysqlBlobOff()},
		{name: "mysql_ddl_dml_rejected", db: "mysql", typ: "ddl", sql: "DELETE FROM t WHERE id=1;", wantOK: false, errRule: "TicketType", rules: mysql},

		// ---- MySQL DML ----
		{name: "mysql_dml_no_where", db: "mysql", typ: "dml", sql: "DELETE FROM t;", errRule: "DMLWhere", rules: mysql},
		{name: "mysql_dml_where_true_ok", db: "mysql", typ: "dml", sql: "UPDATE t SET a=1 WHERE 1=1;", wantOK: true, rules: mysql},
		{name: "mysql_dml_ok", db: "mysql", typ: "dml", sql: "UPDATE t SET a=1 WHERE id=1;", wantOK: true, rules: mysql},
		{name: "mysql_dml_insert_no_cols_default_ok", db: "mysql", typ: "dml", sql: "INSERT INTO t VALUES (1);", wantOK: true, rules: mysql},
		{name: "mysql_dml_insert_no_cols_when_on", db: "mysql", typ: "dml", sql: "INSERT INTO t VALUES (1);", errRule: "DMLInsertMustSpecifyColumn", rules: mysqlInsertColsOn()},
		{name: "mysql_dml_ddl_rejected", db: "mysql", typ: "dml", sql: "CREATE TABLE t (id int);", errRule: "DMLForbidDDL", rules: mysql},

		// ---- MySQL DQL / DCL / 建库 ----
		{name: "mysql_dql_ok", db: "mysql", typ: "dql", sql: "SELECT id, name FROM t WHERE id=1;", wantOK: true, rules: mysql},
		{name: "mysql_dql_with_ok", db: "mysql", typ: "dql", sql: "WITH x AS (SELECT 1 AS a) SELECT a FROM x;", wantOK: true, rules: mysql},
		{name: "mysql_dql_reject_dml", db: "mysql", typ: "dql", sql: "DELETE FROM t WHERE id=1;", errRule: "TicketType", rules: mysql},
		{name: "mysql_dcl_grant", db: "mysql", typ: "dcl", sql: "GRANT SELECT ON t TO u;", wantOK: true, rules: mysql},
		{name: "mysql_dcl_reject_update", db: "mysql", typ: "dcl", sql: "UPDATE t SET a=1 WHERE id=1;", errRule: "TicketType", rules: mysql},
		{name: "mysql_createdb_ok", db: "mysql", typ: "createdb", sql: "CREATE DATABASE db_app;", wantOK: true, createdb: true},
		{name: "mysql_createdb_reject_alter", db: "mysql", typ: "createdb", sql: "ALTER TABLE t ADD c int;", wantOK: false, createdb: true},

		// ---- PostgreSQL 工单类型门禁 ----
		{name: "pg_dql_ok", db: "postgres", typ: "dql", sql: "SELECT id FROM t WHERE id=1;", wantOK: true, rules: pg},
		{name: "pg_dql_explain_ok", db: "postgres", typ: "dql", sql: "EXPLAIN SELECT 1;", wantOK: true, rules: pg},
		{name: "pg_dql_reject_update", db: "postgres", typ: "dql", sql: "UPDATE t SET a=1 WHERE id=1;", errRule: "TicketType", rules: pg},
		{name: "pg_dcl_grant", db: "postgres", typ: "dcl", sql: "GRANT SELECT ON t TO u;", wantOK: true, rules: pg},
		{name: "pg_dcl_reject_select", db: "postgres", typ: "dcl", sql: "SELECT 1;", errRule: "TicketType", rules: pg},
		{name: "pg_createdb_ok", db: "postgres", typ: "createdb", sql: "CREATE DATABASE db_app;", wantOK: true, createdb: true},
		{name: "pg_createdb_reject_ddl", db: "postgres", typ: "createdb", sql: "CREATE TABLE t (id int);", wantOK: false, createdb: true},
		{name: "pg_ddl_ok_add_const", db: "postgres", typ: "ddl", sql: "ALTER TABLE t ADD COLUMN name varchar(20) NOT NULL DEFAULT 'x'; COMMENT ON COLUMN t.name IS 'n';", wantOK: true, rules: pg},
		{name: "pg_dml_ok", db: "postgres", typ: "dml", sql: "UPDATE t SET a=1 WHERE id=1;", wantOK: true, rules: pg},

		// ---- StarRocks 工单类型门禁 ----
		{name: "sr_dql_ok", db: "starrocks", typ: "dql", sql: "SELECT id FROM t WHERE id=1;", wantOK: true, rules: sr},
		{name: "sr_dql_show_ok", db: "starrocks", typ: "dql", sql: "SHOW TABLES;", wantOK: true, rules: sr},
		{name: "sr_dql_reject_insert", db: "starrocks", typ: "dql", sql: "INSERT INTO t (id) VALUES (1);", errRule: "TicketType", rules: sr},
		{name: "sr_dcl_grant", db: "starrocks", typ: "dcl", sql: "GRANT SELECT ON t TO u;", wantOK: true, rules: sr},
		{name: "sr_dcl_reject_ddl", db: "starrocks", typ: "dcl", sql: "ALTER TABLE t ADD COLUMN name varchar(20);", errRule: "TicketType", rules: sr},
		{name: "sr_createdb_ok", db: "starrocks", typ: "createdb", sql: "CREATE DATABASE db_app;", wantOK: true, createdb: true},
		{name: "sr_createdb_reject_dml", db: "starrocks", typ: "createdb", sql: "INSERT INTO t (id) VALUES (1);", wantOK: false, createdb: true},
		{name: "sr_ddl_add_col_ok", db: "starrocks", typ: "ddl", sql: `ALTER TABLE t ADD COLUMN name varchar(20) NOT NULL DEFAULT "x" COMMENT "n";`, wantOK: true, rules: sr},
		{name: "sr_ddl_add_col_missing_comment", db: "starrocks", typ: "ddl", sql: `ALTER TABLE t ADD COLUMN name varchar(20) NOT NULL DEFAULT "x";`, errRule: "DDLCheckColumnComment", rules: sr},
		{name: "sr_dml_ok", db: "starrocks", typ: "dml", sql: "DELETE FROM t WHERE id=1;", wantOK: true, rules: sr},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			var res Result
			if c.createdb {
				res = CheckCreateDB(c.sql)
			} else {
				res = CheckTicketWithRules(c.sql, c.typ, c.db, c.rules)
			}
			if res.OK != c.wantOK {
				t.Fatalf("ok=%v want %v stmts=%+v", res.OK, c.wantOK, res.Statements)
			}
			if c.errRule != "" && !hasRuleID(res, c.errRule) {
				t.Fatalf("errors 应含 %s: %+v", c.errRule, res.Statements)
			}
		})
	}
}
