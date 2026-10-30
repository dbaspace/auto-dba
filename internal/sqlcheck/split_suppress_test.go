package sqlcheck

import (
	"strings"
	"testing"
)

func TestSplitSQL_CreateDatabaseWithEncoding(t *testing.T) {
	sql := `CREATE DATABASE db_ticket_system
    WITH ENCODING = 'UTF8'
         TEMPLATE = template0;`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
	if miss := findMissingSemicolonSplits(sql); len(miss) != 0 {
		t.Fatalf("want no missing-semicolon split, got %#v", miss)
	}
}

func TestSplitSQL_CreateDatabaseIfNotExistsWith(t *testing.T) {
	sql := `CREATE DATABASE IF NOT EXISTS "networkdoctor"
WITH OWNER = postgres
ENCODING = 'UTF8';`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_AlterTableDropColumnNewline(t *testing.T) {
	sql := `ALTER TABLE "public"."tbl_shop_print_v2_plan"
    DROP COLUMN "brand_id";`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
	if miss := findMissingSemicolonSplits(sql); len(miss) != 0 {
		t.Fatalf("want no missing-semicolon split, got %#v", miss)
	}
}

func TestSplitSQL_AlterTableMultiDropColumn(t *testing.T) {
	sql := `ALTER TABLE "public"."tbl_shop_print_v2_plan"
    DROP COLUMN "use_parent_org_id",
    DROP COLUMN "use_brand_id",
    DROP COLUMN "use_brand_name";`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_AlterTableAlterColumnNewline(t *testing.T) {
	sql := `ALTER TABLE "public"."tbl_shop"
    ALTER COLUMN "city" SET DATA TYPE character varying(50);`
	st := SplitSQL(sql)
	if len(st) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(st), st)
	}
}

func TestSplitSQL_AlterTableThenDropTableStillSplits(t *testing.T) {
	// 缺分号时：完整 ALTER 动作后另起 DROP TABLE 仍应分段（真阳性）
	sql := `ALTER TABLE t ADD COLUMN x int
DROP TABLE u;`
	st := SplitSQL(sql)
	if len(st) != 2 {
		t.Fatalf("want 2 statements (ALTER then DROP TABLE), got %d: %#v", len(st), st)
	}
	if !strings.Contains(strings.ToUpper(st[1]), "DROP TABLE") {
		t.Fatalf("second stmt should be DROP TABLE, got %#v", st[1])
	}
}

func TestSQLRequireSemicolon_CreateDatabaseWithNoFalsePositive(t *testing.T) {
	sql := `CREATE DATABASE db_ticket_system
    WITH ENCODING = 'UTF8'
         TEMPLATE = template0;`
	rules := []Rule{
		{Name: "SQLRequireSemicolon", Value: "true"},
		// 允许建库，避免策略失败掩盖拆分问题
		{Name: "DDLForbidCreateDatabase", Value: `{"allow":true}`},
		{Name: "DDLDatabaseNamePrefix", Value: `{"prefix":""}`},
	}
	res := CheckTicketWithRules(sql, "ddl", "postgres", rules)
	for _, row := range res.Statements {
		if row.RuleID == "SQLRequireSemicolon" {
			t.Fatalf("CREATE DATABASE WITH should not hit SQLRequireSemicolon: %+v", row)
		}
		if row.RuleID == "TicketType" {
			t.Fatalf("CREATE DATABASE WITH should not hit TicketType: %+v", row)
		}
	}
}

func TestSQLRequireSemicolon_AlterDropColumnNoFalsePositive(t *testing.T) {
	sql := `ALTER TABLE "public"."tbl_shop_print_v2_plan"
    DROP COLUMN "brand_id";`
	rules := []Rule{
		{Name: "SQLRequireSemicolon", Value: "true"},
		{Name: "DDLAllowDropColumn", Value: `{"allow":true}`},
	}
	res := CheckTicketWithRules(sql, "ddl", "postgres", rules)
	for _, row := range res.Statements {
		if row.RuleID == "SQLRequireSemicolon" {
			t.Fatalf("ALTER TABLE DROP COLUMN newline should not hit SQLRequireSemicolon: %+v", row)
		}
		if row.RuleID == "TicketType" {
			t.Fatalf("unexpected TicketType: %+v", row)
		}
	}
}
