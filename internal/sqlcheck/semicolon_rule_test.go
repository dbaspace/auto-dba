package sqlcheck

import "testing"

func TestSQLRequireSemicolon_MissingBetweenUpdates(t *testing.T) {
	sql := `UPDATE t SET a = 1 WHERE id = 1
UPDATE t SET a = 2 WHERE id = 2;`
	rules := []Rule{
		{Name: "DMLWhere", Value: "true"},
		{Name: "SQLRequireSemicolon", Value: "true"},
	}
	res := CheckTicketWithRules(sql, "dml", "postgres", rules)
	if res.OK {
		t.Fatalf("expected fail on missing semicolon, got OK statements=%+v", res.Statements)
	}
	found := false
	for _, row := range res.Statements {
		if row.RuleID == "SQLRequireSemicolon" && row.ErrorLevel == "ERROR" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected SQLRequireSemicolon ERROR, got %+v", res.Statements)
	}
}

func TestSQLRequireSemicolon_WithSemicolonPasses(t *testing.T) {
	sql := `UPDATE t SET a = 1 WHERE id = 1;
UPDATE t SET a = 2 WHERE id = 2;`
	rules := []Rule{
		{Name: "DMLWhere", Value: "true"},
		{Name: "SQLRequireSemicolon", Value: "true"},
	}
	res := CheckTicketWithRules(sql, "dml", "postgres", rules)
	if !res.OK {
		t.Fatalf("expected OK, got errors=%+v statements=%+v", res.Errors, res.Statements)
	}
	for _, row := range res.Statements {
		if row.RuleID == "SQLRequireSemicolon" {
			t.Fatalf("unexpected semicolon hit: %+v", row)
		}
	}
}

func TestSQLRequireSemicolon_DisabledAllowsMissing(t *testing.T) {
	sql := `UPDATE t SET a = 1 WHERE id = 1
UPDATE t SET a = 2 WHERE id = 2;`
	rules := []Rule{
		{Name: "DMLWhere", Value: "true"},
		{Name: "SQLRequireSemicolon", Value: "false"},
	}
	res := CheckTicketWithRules(sql, "dml", "postgres", rules)
	if !res.OK {
		t.Fatalf("rule disabled should pass, got %+v", res.Statements)
	}
}

func TestSQLRequireSemicolon_CreateViewASNotFalsePositive(t *testing.T) {
	sql := `CREATE OR REPLACE VIEW v
AS
SELECT 1 AS x;`
	rules := []Rule{{Name: "SQLRequireSemicolon", Value: "true"}}
	res := CheckTicketWithRules(sql, "ddl", "postgres", rules)
	for _, row := range res.Statements {
		if row.RuleID == "SQLRequireSemicolon" {
			t.Fatalf("CREATE VIEW AS\\nSELECT should not hit semicolon rule: %+v", row)
		}
	}
}

func TestFindMissingSemicolonSplits_CTEThenUpdate(t *testing.T) {
	// WITH ... UPDATE 应抑制误拆；WITH 完成后的下一句 UPDATE 若缺分号仍应检出
	sql := `WITH c AS (SELECT 1 AS id)
UPDATE t SET a = 1 WHERE id IN (SELECT id FROM c)
UPDATE t2 SET b = 2 WHERE id = 2;`
	snips := findMissingSemicolonSplits(sql)
	if len(snips) != 1 {
		t.Fatalf("want 1 missing-semicolon boundary after CTE+UPDATE, got %d: %#v", len(snips), snips)
	}
}

func TestFindMissingSemicolonSplits_NoneWhenSemicolonsPresent(t *testing.T) {
	sql := `UPDATE t SET a = 1 WHERE id = 1;
UPDATE t SET a = 2 WHERE id = 2;`
	if snips := findMissingSemicolonSplits(sql); len(snips) != 0 {
		t.Fatalf("want 0, got %#v", snips)
	}
}
