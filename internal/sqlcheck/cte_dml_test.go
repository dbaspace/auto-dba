package sqlcheck

import (
	"strings"
	"testing"
)

func TestDMLWithUpdateNotMultiTable(t *testing.T) {
	sql := `with c as (select order_id, business_date from tbl_order_master where is_deleted = true)
update t sub SET origin_business_date = c.business_date, is_deleted = true FROM c where is_deleted = false and sub.order_id = c.order_id;`
	rules := []Rule{{Name: "DMLAllowMultiTable", Value: "false"}}
	res := CheckTicketWithRules(sql, "dml", "postgres", rules)
	if !res.OK || hasRuleID(res, "TicketType") || hasRuleID(res, "DMLAllowMultiTable") {
		t.Fatalf("WITH UPDATE should pass as single-table DML: %+v", res)
	}
	if len(res.Statements) != 1 || !strings.Contains(strings.ToLower(res.Statements[0].SQL), "update ") {
		t.Fatalf("want one combined statement, got %+v", res.Statements)
	}

	multi := `update a set x=1 from b where a.id = b.id;`
	res = CheckTicketWithRules(multi, "dml", "postgres", rules)
	if !hasRuleID(res, "DMLAllowMultiTable") {
		t.Fatalf("plain UPDATE FROM real table should warn: %+v", res)
	}
}

func TestDMLWithUpdateMultipleCTEs(t *testing.T) {
	rules := []Rule{{Name: "DMLAllowMultiTable", Value: "false"}}
	sql := `WITH a AS (select 1 AS id),
b AS (select 2 AS id)
UPDATE t SET x = 1 FROM a, b WHERE t.id = a.id;`
	res := CheckTicketWithRules(sql, "dml", "postgres", rules)
	if !res.OK || hasRuleID(res, "DMLAllowMultiTable") || hasRuleID(res, "TicketType") {
		t.Fatalf("FROM only CTEs should not be multi-table: %+v", res)
	}

	mixed := `WITH a AS (select 1 AS id)
UPDATE t SET x = 1 FROM a, real_table WHERE t.id = a.id;`
	res = CheckTicketWithRules(mixed, "dml", "postgres", rules)
	if !hasRuleID(res, "DMLAllowMultiTable") {
		t.Fatalf("FROM CTE plus real table should warn: %+v", res)
	}
}

func TestDMLTautologySwitch(t *testing.T) {
	user := `update tbl_order_master set is_deleted = true, origin_business_date = business_date where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and origin_business_date <= '2026-10-16';`
	withUpdate := `with clear_order as (select order_id from tbl_order_master where is_deleted = true)
update tbl_order_item_master sub SET is_deleted = true FROM clear_order where is_deleted = false and sub.order_id = clear_order.order_id;`
	on := []Rule{{Name: "DMLForbidTautology", Value: "true"}, {Name: "DMLWhere", Value: "true"}}
	for _, sql := range []string{user, withUpdate} {
		res := CheckTicketWithRules(sql, "dml", "postgres", on)
		if hasRuleID(res, "DMLForbidTautology") {
			t.Fatalf("should not treat column = true as tautology: %s %+v", sql, res)
		}
	}
	if res := CheckTicketWithRules("UPDATE t SET a=1 WHERE 1=1;", "dml", "postgres", on); !hasRuleID(res, "DMLForbidTautology") {
		t.Fatalf("WHERE 1=1 should be rejected when switch is on: %+v", res)
	}
	if res := CheckTicketWithRules("UPDATE t SET a=1 WHERE id=1 OR (1 = 1);", "dml", "postgres", on); !hasRuleID(res, "DMLForbidTautology") {
		t.Fatalf("OR 1=1 should be rejected: %+v", res)
	}
	if res := CheckTicketWithRules("UPDATE t SET a=1 WHERE id=11;", "dml", "postgres", on); hasRuleID(res, "DMLForbidTautology") {
		t.Fatalf("id=11 must not match 1=1: %+v", res)
	}
	off := []Rule{{Name: "DMLForbidTautology", Value: "false"}, {Name: "DMLWhere", Value: "true"}}
	if res := CheckTicketWithRules("UPDATE t SET a=1 WHERE 1=1;", "dml", "postgres", off); hasRuleID(res, "DMLForbidTautology") || !res.OK {
		t.Fatalf("switch off should allow WHERE 1=1: %+v", res)
	}
}

func TestMainKeywordWithUpdate(t *testing.T) {
	stmt := `with c (id) as not materialized (select 1)
update t set a = 1 from c where t.id = c.id`
	if mainKeyword(stmt) != "UPDATE" {
		t.Fatalf("main keyword=%s", mainKeyword(stmt))
	}
	names := cteNames(stmt)
	if len(names) != 1 || names[0] != "c" {
		t.Fatalf("cte names=%v", names)
	}
	if mainKeyword("UPDATE t SET a=1 WHERE id=1") != "UPDATE" {
		t.Fatal("plain update keyword changed")
	}
}
