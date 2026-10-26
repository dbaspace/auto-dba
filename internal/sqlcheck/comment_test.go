package sqlcheck

import (
	"strings"
	"testing"
)

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

func TestPostgresAddColumnIfNotExistsComment(t *testing.T) {
	rules := []Rule{
		{Name: "DDLCheckColumnComment", Value: "true"},
		{Name: "DDLCheckTableComment", Value: "true"},
	}
	sql := `ALTER TABLE tbl_order_0 ADD COLUMN IF NOT EXISTS order_serial_no TEXT, ADD COLUMN IF NOT EXISTS pay_channel TEXT;
COMMENT ON COLUMN tbl_order_0.order_serial_no IS '订单流水号';
COMMENT ON COLUMN tbl_order_0.pay_channel IS '支付渠道';`
	ok := CheckTicketWithRules(sql, "ddl", "postgres", rules)
	if !ok.OK {
		t.Fatalf("ADD COLUMN IF NOT EXISTS 且两条注释都在应通过: %+v", ok)
	}
	missingOne := CheckTicketWithRules(
		`ALTER TABLE tbl_order_0 ADD COLUMN IF NOT EXISTS order_serial_no TEXT, ADD COLUMN IF NOT EXISTS pay_channel TEXT;
COMMENT ON COLUMN tbl_order_0.order_serial_no IS '订单流水号';`,
		"ddl", "postgres", rules,
	)
	if missingOne.OK || !hasRuleID(missingOne, "DDLCheckColumnComment") {
		t.Fatalf("去掉其中一条注释应失败: %+v", missingOne)
	}
	empty := CheckTicketWithRules(
		`ALTER TABLE tbl_order_0 ADD COLUMN IF NOT EXISTS order_serial_no TEXT; COMMENT ON COLUMN tbl_order_0.order_serial_no IS '';`,
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

func ruleMessage(res Result, id string) string {
	for _, st := range res.Statements {
		if strings.EqualFold(st.RuleID, id) {
			return st.Message
		}
	}
	return ""
}

func TestPostgresScreenshotAddColumnComment(t *testing.T) {
	rules := []Rule{
		{Name: "DDLCheckColumnComment", Value: "true", Label: "列必须有注释"},
		{Name: "DDLCheckTableComment", Value: "true", Label: "表必须有注释"},
		{Name: "DDLIndexCreateConcurrently", Value: "500000", Label: "索引 CREATE/DROP 建议 CONCURRENTLY"},
	}
	sql := `ALTER TABLE public.tbl_lxz_discount_account
ADD COLUMN IF NOT EXISTS corporation_id bigint NOT NULL DEFAULT 0;
COMMENT ON COLUMN public.tbl_lxz_discount_account.corporation_id IS '集团 ID，来自请求头 corporation-id';
DROP INDEX IF EXISTS public.uk_lxz_discount_account_name;
CREATE UNIQUE INDEX IF NOT EXISTS uk_lxz_discount_account_corp_name
ON public.tbl_lxz_discount_account (corporation_id, discount_name)
WHERE is_archived = false AND is_deleted = false AND discount_name <> '';`
	ok := CheckTicketWithRules(sql, "ddl", "postgres", rules)
	if !ok.OK || hasRuleID(ok, "DDLCheckColumnComment") {
		t.Fatalf("PostgreSQL 换行 ADD COLUMN IF NOT EXISTS 且已有 COMMENT ON COLUMN 应通过: %+v", ok)
	}
	if !hasRuleID(ok, "DDLIndexCreateConcurrently") {
		t.Fatalf("索引缺少 CONCURRENTLY 应保留警告: %+v", ok)
	}
	for _, st := range ok.Statements {
		if st.RuleID == "DDLIndexCreateConcurrently" && st.ErrorLevel != "WARN" {
			t.Fatalf("索引 CONCURRENTLY 应为警告: %+v", st)
		}
	}
	missing := CheckTicketWithRules(`ALTER TABLE public.tbl_lxz_discount_account
ADD COLUMN IF NOT EXISTS corporation_id bigint NOT NULL DEFAULT 0;`, "ddl", "postgres", rules)
	msg := ruleMessage(missing, "DDLCheckColumnComment")
	if missing.OK || !strings.Contains(msg, "corporation_id") || !strings.Contains(msg, "COMMENT ON COLUMN") {
		t.Fatalf("PostgreSQL 缺注释应写出列名和 COMMENT ON COLUMN: %+v", missing)
	}
}

func TestCommentRuleFollowsEngine(t *testing.T) {
	sql := `ALTER TABLE public.tbl_lxz_discount_account
ADD COLUMN IF NOT EXISTS corporation_id bigint NOT NULL DEFAULT 0;
COMMENT ON COLUMN public.tbl_lxz_discount_account.corporation_id IS '集团 ID';`
	rules := []Rule{{Name: "DDLCheckColumnComment", Value: "true", Label: "列必须有注释"}}
	pg := CheckTicketWithRules(sql, "ddl", "postgresql", rules)
	if !pg.OK || hasRuleID(pg, "DDLCheckColumnComment") {
		t.Fatalf("PostgreSQL 应认 COMMENT ON COLUMN: %+v", pg)
	}
	sr := CheckTicketWithRules(sql, "ddl", "starrocks", rules)
	msg := ruleMessage(sr, "DDLCheckColumnComment")
	if sr.OK || !strings.Contains(msg, "COMMENT '...'") || strings.Contains(msg, "COMMENT ON COLUMN") {
		t.Fatalf("StarRocks 应要求行内注释，不认 COMMENT ON COLUMN: %+v", sr)
	}
}
