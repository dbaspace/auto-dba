package sqlcheck

import "testing"

func TestPostgresCommentOnColumnRequired(t *testing.T) {
	rules := []Rule{
		{Name: "DDLCheckColumnComment", Value: "true"},
		{Name: "DDLCheckTableComment", Value: "true"},
	}
	miss := CheckTicketWithRules("ALTER TABLE tbl_test ADD COLUMN xx int NOT NULL DEFAULT '0';", "ddl", "postgres", rules)
	if miss.OK || !hasRuleID(miss, "DDLCheckColumnComment") {
		t.Fatalf("缺少 COMMENT ON COLUMN 应失败: %+v", miss)
	}
	ok := CheckTicketWithRules(
		"ALTER TABLE tbl_test ADD COLUMN xx int NOT NULL DEFAULT '0'; COMMENT ON COLUMN tbl_test.xx IS 'desc';",
		"ddl", "postgres", rules,
	)
	if !ok.OK {
		t.Fatalf("有 COMMENT ON COLUMN 应通过: %+v", ok)
	}
	quoted := CheckTicketWithRules(
		`ALTER TABLE "public"."tbl_test" ADD COLUMN name varchar(20) NOT NULL DEFAULT ''; COMMENT ON COLUMN tbl_test.name IS 'n';`,
		"ddl", "postgres", rules,
	)
	if !quoted.OK {
		t.Fatalf("混用表名写法应通过: %+v", quoted)
	}
	empty := CheckTicketWithRules(
		"ALTER TABLE tbl_test ADD COLUMN xx int NOT NULL DEFAULT '0'; COMMENT ON COLUMN tbl_test.xx IS '';",
		"ddl", "postgres", rules,
	)
	if empty.OK {
		t.Fatalf("空注释不应通过: %+v", empty)
	}
}

func TestPostgresCreateTableComments(t *testing.T) {
	rules := []Rule{
		{Name: "DDLCheckColumnComment", Value: "true"},
		{Name: "DDLCheckTableComment", Value: "true"},
	}
	miss := CheckTicketWithRules("CREATE TABLE tbl_prod_name(id bigserial primary key);", "ddl", "postgres", rules)
	if miss.OK {
		t.Fatalf("建表无注释应失败: %+v", miss)
	}
	ok := CheckTicketWithRules(
		"CREATE TABLE tbl_prod_name(id bigserial primary key); COMMENT ON TABLE tbl_prod_name IS 't'; COMMENT ON COLUMN tbl_prod_name.id IS 'pk';",
		"ddl", "postgres", rules,
	)
	if !ok.OK {
		t.Fatalf("建表有表/列注释应通过: %+v", ok)
	}
}

func TestStarRocksInlineColumnComment(t *testing.T) {
	rules := []Rule{
		{Name: "DDLCheckColumnComment", Value: "true"},
		{Name: "DDLCheckTableComment", Value: "true"},
		{Name: "DDLAddColumnRequireDefault", Value: "true"},
	}
	miss := CheckTicketWithRules(`ALTER TABLE t ADD COLUMN xx INT NOT NULL DEFAULT "0";`, "ddl", "starrocks", rules)
	if miss.OK || !hasRuleID(miss, "DDLCheckColumnComment") {
		t.Fatalf("SR 加列无 COMMENT 应失败: %+v", miss)
	}
	ok := CheckTicketWithRules(`ALTER TABLE t ADD COLUMN xx INT NOT NULL DEFAULT "0" COMMENT "col";`, "ddl", "starrocks", rules)
	if !ok.OK {
		t.Fatalf("SR 加列有 COMMENT 应通过: %+v", ok)
	}
}

func TestCommentRuleOff(t *testing.T) {
	rules := []Rule{{Name: "DDLCheckColumnComment", Value: "false"}}
	res := CheckTicketWithRules("ALTER TABLE t ADD COLUMN xx int NOT NULL DEFAULT '0';", "ddl", "postgres", rules)
	if !res.OK {
		t.Fatalf("关闭列注释规则应通过: %+v", res)
	}
}
