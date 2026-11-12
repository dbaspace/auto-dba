package sqlcheck

import (
	"strings"
	"testing"
)

func TestSplitSQL_InsertSelectStaysOneStatement(t *testing.T) {
	sql := `INSERT INTO public.tbl_order_discount_48 (id, parent, order_id)
SELECT p.id, p.parent, d.order_id
FROM (VALUES (1, 2, 3), (4, 5, 6)) AS p(id, parent, order_id);`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
	if !strings.Contains(strings.ToUpper(st[0]), "SELECT") {
		t.Fatalf("SELECT 应留在 INSERT 里: %s", st[0])
	}
	dml := CheckTicket(sql, "dml", false, false)
	for _, row := range dml.Statements {
		if row.ErrorLevel == "ERROR" && strings.Contains(row.Message, "必须是 DML") {
			t.Fatalf("INSERT SELECT 不应判成非 DML: %+v", row)
		}
	}
}

func TestSplitSQL_InsertValuesThenSelectStillSplits(t *testing.T) {
	sql := `INSERT INTO t (a) VALUES (1)
SELECT 1;`
	st := SplitSQL(sql)
	if len(st) != 2 {
		t.Fatalf("want 2 statements, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_InsertSelectThenAnotherSelectSplits(t *testing.T) {
	sql := `INSERT INTO t
SELECT 1
SELECT 2;`
	st := SplitSQL(sql)
	if len(st) != 2 {
		t.Fatalf("want 2 statements, got %d: %#v", len(st), st)
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(st[0])), "INSERT") || !strings.Contains(strings.ToUpper(st[0]), "SELECT") {
		t.Fatalf("第一条应是 INSERT SELECT: %s", st[0])
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(st[1])), "SELECT") {
		t.Fatalf("第二条应是 SELECT: %s", st[1])
	}
}

func TestCountInsertRowsIgnoresQueryValues(t *testing.T) {
	stmt := `INSERT INTO t (id)
SELECT p.id
FROM (VALUES (1), (2), (3)) AS p(id)`
	if n := countInsertRows(stmt); n != 0 {
		t.Fatalf("INSERT SELECT 行数应为 0，got %d", n)
	}
	if n := countInsertRows(`INSERT INTO t (id) VALUES (1),(2),(3)`); n != 3 {
		t.Fatalf("VALUES 行数应为 3，got %d", n)
	}
}

func TestSplitSQL_OrderDiscountRepairStaysFourStatements(t *testing.T) {
	sql := `-- 1. 旧优惠失效
UPDATE public.tbl_order_discount_48 SET is_deleted=TRUE,update_time=CURRENT_TIMESTAMP
WHERE corporation_id=1 AND is_deleted=FALSE;
-- 2. 从模板复制
INSERT INTO public.tbl_order_discount_48(discount_item_id,parent_discount_item_id,order_id,discount_amount,origin_business_date)
SELECT p.id,p.parent,d.order_id,p.ordinary,d.origin_business_date
FROM (VALUES (1794381502704,1794381502704,0,0,2998,1794233385472),
 (1794381502705,1794381502704,20,1794233395752,2998,1794233385476)) p(id,parent,scope,scope_id,ordinary,template)
JOIN public.tbl_order_discount_48 d ON d.order_id=1 AND d.discount_item_id=p.template
WHERE d.corporation_id=1 AND d.is_deleted=FALSE
 AND NOT EXISTS(SELECT 1 FROM public.tbl_order_discount_48 x WHERE x.order_id=d.order_id AND x.discount_item_id=p.id);
-- 3. 更新商品
UPDATE public.tbl_order_item_master_48 t SET discount_amount=v.ordinary,actual_amount=0,update_time=CURRENT_TIMESTAMP
FROM (VALUES (1794233395752,2998,2),
 (1794233395754,1499,1)) v(id,ordinary,share)
WHERE t.item_id=v.id AND t.discount_amount=0;
-- 4. 更新订单
UPDATE public.tbl_order_master_48 SET update_time=CURRENT_TIMESTAMP
WHERE order_id=1 AND version=4;`
	st := SplitSQL(sql)
	if len(st) != 4 {
		t.Fatalf("want 4 statements, got %d: %#v", len(st), st)
	}
	ins := strings.TrimSpace(st[1])
	insUpper := strings.ToUpper(ins)
	if !strings.HasPrefix(insUpper, "INSERT") || !strings.Contains(insUpper, "SELECT") {
		t.Fatalf("第 2 条应是 INSERT SELECT: %s", ins)
	}
	if !strings.Contains(insUpper, "JOIN") || !strings.Contains(insUpper, "NOT EXISTS") {
		t.Fatalf("查询体应留在 INSERT 里: %s", ins)
	}
	if strings.HasSuffix(strings.TrimRight(ins, "; \t\r\n"), ")") && !strings.Contains(insUpper, "SELECT") {
		t.Fatalf("INSERT 停在列清单: %s", ins)
	}
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(st[2])), "UPDATE") || !strings.Contains(strings.ToUpper(st[2]), "VALUES") {
		t.Fatalf("第 3 条应是 UPDATE FROM VALUES: %s", st[2])
	}
	if FirstIncompleteInsert(st) != 0 {
		t.Fatalf("完整脚本不应有不完整 INSERT")
	}
	dml := CheckTicket(sql, "dml", false, false)
	for _, row := range dml.Statements {
		if row.ErrorLevel == "ERROR" && (strings.Contains(row.Message, "INSERT 不完整") || strings.Contains(row.Message, "必须是 DML")) {
			t.Fatalf("完整脚本不应报拆句错误: %+v", row)
		}
	}
}

func TestCheckTicketIncompleteInsert(t *testing.T) {
	bad := CheckTicket("UPDATE t SET a=1 WHERE id=1;\nINSERT INTO t (a);", "dml", false, false)
	if bad.OK {
		t.Fatal("只有列清单的 INSERT 应失败")
	}
	var hit bool
	for _, row := range bad.Statements {
		if row.ErrorLevel == "ERROR" && strings.Contains(row.Message, "INSERT 不完整：列清单后缺少 VALUES 或 SELECT") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("缺少不完整 INSERT 错误: %+v", bad.Statements)
	}
	for _, sql := range []string{
		`INSERT INTO t (a) VALUES (1)`,
		"INSERT INTO t (a)\nSELECT 1",
		`REPLACE INTO t (a) VALUES (1)`,
	} {
		res := CheckTicket(sql, "dml", false, false)
		for _, row := range res.Statements {
			if strings.Contains(row.Message, "INSERT 不完整") {
				t.Fatalf("%s 不应判为不完整: %+v", sql, row)
			}
		}
	}
	rep := CheckTicket("REPLACE INTO t (a)", "dml", false, false)
	hit = false
	for _, row := range rep.Statements {
		if strings.Contains(row.Message, "INSERT 不完整") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("REPLACE 列清单应判为不完整: %+v", rep.Statements)
	}
}

func TestSplitSQL_CreateViewUnionAll(t *testing.T) {
	sql := `CREATE OR REPLACE VIEW db_ods.v AS
SELECT a FROM t1
WHERE x = 20
UNION ALL
SELECT a FROM t2
WHERE x = 40;`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_CreateViewUnionAllProductionSkeleton(t *testing.T) {
	sql := `create or replace view db_ods.tbl_stored_value_package_consumption_view as
SELECT trans_account.stored_value_program_id, trans_detail.card_id
FROM ods_tbl_crm_recharge_transaction_detail trans_detail
LEFT JOIN db_ods.ods_tbl_crm_recharge_transaction_account trans_account
  ON trans_detail.corporation_id = trans_account.corporation_id
WHERE trans_detail.trans_type = 20
UNION ALL
SELECT trans_account.stored_value_program_id, trans_detail.card_id
FROM ods_tbl_crm_recharge_transaction_detail trans_detail
LEFT JOIN db_ods.ods_tbl_crm_recharge_transaction_account trans_account
  ON trans_detail.corporation_id = trans_account.corporation_id
WHERE trans_detail.trans_type = 40;`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_CreateViewASOnOwnLine(t *testing.T) {
	sql := `CREATE OR REPLACE VIEW tbl_order_item_view_rt
as
select 1;`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_SelectAliasStillSplits(t *testing.T) {
	sql := `SELECT 1 AS x
SELECT 2;`
	st := SplitSQL(sql)
	if len(st) != 2 {
		t.Fatalf("want 2 statements, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_ExceptAndIntersect(t *testing.T) {
	for _, op := range []string{"EXCEPT", "EXCEPT ALL", "INTERSECT", "INTERSECT ALL"} {
		sql := "CREATE VIEW v AS\nSELECT 1\n" + op + "\nSELECT 2;"
		st := SplitSQL(sql)
		if len(st) != 1 {
			t.Fatalf("%s: want 1 statement, got %d: %#v", op, len(st), st)
		}
	}
}

func TestSplitSQL_UserSRViewInlineUnion(t *testing.T) {
	sql := `create or replace view db_ods.tbl_stored_value_package_consumption_view as SELECT a FROM t WHERE x = 20 UNION ALL SELECT a FROM t WHERE x = 40;`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_LeadingLineCommentNotInStatement(t *testing.T) {
	sql := `-- 对方的
alter table tbl_test add column xx int;`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
	if strings.Contains(st[0], "--") || strings.Contains(st[0], "对方的") {
		t.Fatalf("comment must not appear in statement text: %q", st[0])
	}
	if !strings.Contains(strings.ToLower(st[0]), "alter table") {
		t.Fatalf("want ALTER TABLE body, got %q", st[0])
	}
}

func TestSplitSQL_WithUpdateIsOneStatement(t *testing.T) {
	sql := `update tbl_order_master set is_deleted = true, origin_business_date = business_date where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and origin_business_date <= '2026-10-16';

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_item_master sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_item_modifier sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_payment sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_surcharge sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_tax sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_discount sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_refund_master sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_refund_item_master sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_refund_item_modifier sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_refund_payment sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_refund_discount sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
update tbl_order_refund_surcharge sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;

with clear_order as (select order_id, business_date from tbl_order_master where is_deleted = true and corporation_id = 101 and shop_id in (407175243))
UPDATE tbl_order_refund_tax as sub SET origin_business_date = clear_order.business_date, is_deleted = true FROM clear_order where is_deleted = false and corporation_id = 101 and shop_id in (407175243) and sub.order_id = clear_order.order_id;`
	st := SplitSQL(sql)
	if len(st) != 14 {
		t.Fatalf("want 14 statements, got %d: %#v", len(st), st)
	}
	if strings.Contains(strings.ToLower(st[0]), "with ") {
		t.Fatalf("first statement should be plain UPDATE: %s", st[0])
	}
	for i, stmt := range st[1:] {
		low := strings.ToLower(stmt)
		if !strings.HasPrefix(low, "with ") || !strings.Contains(low, "update ") {
			t.Fatalf("stmt %d should keep WITH and UPDATE together: %s", i+2, stmt)
		}
	}
}

func TestSplitSQL_WithSelectThenUpdateStillSplits(t *testing.T) {
	sql := `WITH x AS (select 1) SELECT * FROM x
UPDATE t SET a=1 WHERE id=1;`
	st := SplitSQL(sql)
	if len(st) != 2 {
		t.Fatalf("want 2 statements, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_OrderDiscountInsertSelectStaysWithUpdates(t *testing.T) {
	sql := `UPDATE public.tbl_order_master_33 m
SET total_payment_discount_amount=1307, update_time=CURRENT_TIMESTAMP
WHERE m.corporation_id=302 AND m.shop_id=77165409
  AND m.order_id=2118876210776351329 AND m.local_order_id='2026110710387714565210004'
  AND m.origin_business_date=DATE '2026-11-07' AND m.is_deleted=false
  AND m.total_payment_discount_amount=0 AND m.net_amount=4423
  AND m.total_item_amount=6310 AND m.total_discount_amount=880
  AND m.status=20 AND m.close_status=1 AND m.version=1;

-- 2. 四菜支付分摊，预期 UPDATE 4。
UPDATE public.tbl_order_item_master_33 i
SET total_share_discount_amount=v.share, actual_amount=v.new_actual,
    update_time=CURRENT_TIMESTAMP
FROM (VALUES
    (2118876210776350727,4690,655,4035,972,3063),
    (2118876210776350728,300,43,257,62,195)
) v(item_id,old_gross,old_discount,old_actual,share,new_actual)
WHERE i.corporation_id=302 AND i.shop_id=77165409
  AND i.order_id=2118876210776351329 AND i.item_id=v.item_id;

-- 3. 补佣金优惠明细，列清单换行后接 SELECT。
INSERT INTO public.tbl_order_discount_33 (
  discount_item_id,parent_discount_item_id,order_id,local_order_id,corporation_id,shop_id,
  scope,scope_id,discount_id,discount_code,discount_name,discount_type,
  applied_type,applied_id,processing_state,processing_scope,source_type,
  discount_amount,actual_discount_amount,share_revenue_amount,discount_revenue_amount,
  applied_server_id,applied_server_name,approver_server_id,approver_server_name,
  created_by,modified_by,triggers_json,metadata,business_date,origin_business_date
)
SELECT v.discount_item_id,1794477412268,2118876210776351329,'2026110710387714565210004',302,77165409,
  v.scope,v.scope_id,'51012','','美团外卖佣金/服务费',22,
  3,'2118876210809905161',2,0,'5101',
  0,v.actual_discount,0,0,
  0,'',0,'','oncall-repair','oncall-repair','{}'::json,
  '{"properties":{},"discountNameTranslate":{"domain":"BOH_METADATA","resource":"discountType","key":"51012"}}'::json,'2026-11-07',DATE '2026-11-07'
FROM (VALUES
    (1794477412268,0,0,1307),
    (1794477412269,20,2118876210776350727,972)
) v(discount_item_id,scope,scope_id,actual_discount);`
	st := SplitSQL(sql)
	if len(st) != 3 {
		t.Fatalf("want 3 statements, got %d: %#v", len(st), st)
	}
	third := strings.ToUpper(st[2])
	if !strings.Contains(third, "SELECT") || !strings.Contains(third, "VALUES") {
		t.Fatalf("第 3 条应同时包含 SELECT 和 VALUES: %s", st[2])
	}
	if FirstIncompleteInsert(st) != 0 {
		t.Fatalf("第 3 条不应被判成不完整 INSERT")
	}
}

func TestSplitSQL_BlockCommentNotInStatement(t *testing.T) {
	sql := `/* note */ CREATE TABLE tbl_t (id int);`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
	if strings.Contains(st[0], "/*") || strings.Contains(st[0], "note") {
		t.Fatalf("block comment must not appear: %q", st[0])
	}
}
