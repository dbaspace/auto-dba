package sqlcheck

import (
	"strings"
	"testing"
)

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
