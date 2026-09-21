package sqlcheck

import (
	"os"
	"strings"
	"testing"
)

func updateTimeRules() []Rule {
	rules := configuredPGRules()
	for i := range rules {
		if rules[i].Name == "DDLRequireUpdateTime" {
			rules[i].Value = `{"enabled":true,"column":"update_time","allowed_types":["timestamp","timestamptz","timestamp without time zone","timestamp with time zone"]}`
		}
		// 降低无关规则干扰：注释/前缀等在迷你用例中可关闭
		if rules[i].Name == "DDLCheckColumnComment" || rules[i].Name == "DDLCheckTableComment" {
			rules[i].Value = "false"
		}
		if rules[i].Name == "DDLCheckColumnNullable" || rules[i].Name == "DDLCheckColumnDefault" {
			rules[i].Value = "false"
		}
		if rules[i].Name == "DDLCheckPrimaryKeyExists" {
			rules[i].Value = "false"
		}
		if rules[i].Name == "DDLTableNamePrefix" {
			rules[i].Value = ""
		}
	}
	return rules
}

func TestSplitSQL_SkipsLineComments(t *testing.T) {
	stmts := SplitSQL(`
-- leading comment
CREATE TABLE t (id int);
/* block */
-- trailing
`)
	if len(stmts) != 1 {
		t.Fatalf("stmts=%d want 1: %+v", len(stmts), stmts)
	}
	if firstKeyword(stmts[0]) != "CREATE" {
		t.Fatalf("keyword=%q", firstKeyword(stmts[0]))
	}
}

func TestSplitSQL_IgnoresSemicolonInString(t *testing.T) {
	stmts := SplitSQL(`INSERT INTO t (a) VALUES ('a;b'); SELECT 1;`)
	if len(stmts) != 2 {
		t.Fatalf("stmts=%d want 2: %+v", len(stmts), stmts)
	}
}

func TestRequireUpdateTime_MissingColumn(t *testing.T) {
	res := CheckTicketWithRules(
		`CREATE TABLE tbl_t (id bigserial PRIMARY KEY);`,
		"ddl", "postgres", updateTimeRules(),
	)
	if res.OK || !hasRuleID(res, "DDLRequireUpdateTime") {
		t.Fatalf("want update_time error, got ok=%v stmts=%+v", res.OK, res.Statements)
	}
	if !strings.Contains(res.Statements[0].Message, "缺少列") {
		t.Fatalf("msg=%s", res.Statements[0].Message)
	}
}

func TestRequireUpdateTime_MissingTrigger(t *testing.T) {
	sql := `
CREATE TABLE tbl_t (
  id bigserial PRIMARY KEY,
  update_time timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_t_update_time ON tbl_t (update_time);
`
	res := CheckTicketWithRules(sql, "ddl", "postgres", updateTimeRules())
	if res.OK || !hasRuleID(res, "DDLRequireUpdateTime") {
		t.Fatalf("want trigger error, got ok=%v stmts=%+v", res.OK, res.Statements)
	}
	if !strings.Contains(joinedErrors(res), "BEFORE UPDATE") {
		t.Fatalf("msg=%s", joinedErrors(res))
	}
}

func TestRequireUpdateTime_MissingIndex(t *testing.T) {
	sql := `
CREATE TABLE tbl_t (
  id bigserial PRIMARY KEY,
  update_time timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER trg_t_ut BEFORE UPDATE ON tbl_t FOR EACH ROW EXECUTE FUNCTION set_update_time();
`
	res := CheckTicketWithRules(sql, "ddl", "postgres", updateTimeRules())
	if res.OK || !hasRuleID(res, "DDLRequireUpdateTime") {
		t.Fatalf("want index error, got ok=%v stmts=%+v", res.OK, res.Statements)
	}
	if !strings.Contains(joinedErrors(res), "缺少索引") {
		t.Fatalf("msg=%s", joinedErrors(res))
	}
}

func TestRequireUpdateTime_PassWithUsingBtree(t *testing.T) {
	sql := `
CREATE TABLE public.tbl_finance_order (
  id bigserial PRIMARY KEY,
  update_time timestamptz not null default current_timestamp
);
CREATE TRIGGER tbl_finance_order_auto_update_time
  BEFORE UPDATE ON public.tbl_finance_order
  FOR EACH ROW EXECUTE FUNCTION public.auto_update_time();
CREATE INDEX IF NOT EXISTS idx_finance_order_update_time
  ON public.tbl_finance_order USING btree (update_time);
`
	res := CheckTicketWithRules(sql, "ddl", "postgres", updateTimeRules())
	if hasRuleID(res, "DDLRequireUpdateTime") {
		t.Fatalf("USING btree should satisfy update_time rule, got %+v", res.Statements)
	}
}

func TestRequireUpdateTime_PassWithLeadingComments(t *testing.T) {
	sql := `
-- 财务建表脚本
-- 含注释应可通过
CREATE TABLE public.tbl_t (
  id bigserial PRIMARY KEY,
  update_time timestamptz not null default now()
);
CREATE INDEX idx_t_update_time ON public.tbl_t USING btree (update_time);
CREATE TRIGGER trg_t BEFORE UPDATE ON public.tbl_t FOR EACH ROW EXECUTE FUNCTION public.auto_update_time();
`
	res := CheckTicketWithRules(sql, "ddl", "postgres", updateTimeRules())
	if hasRuleID(res, "DDLRequireUpdateTime") {
		t.Fatalf("commented SQL should pass update_time, got %+v", res.Statements)
	}
}

func TestRequireUpdateTime_FinanceSQLFile(t *testing.T) {
	path := "/Users/chenjiachi/Downloads/001_create_finance_sync_tables.sql"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("finance SQL fixture not present:", err)
	}
	rules := updateTimeRules()
	// 真实脚本带完整注释；打开注释规则即可
	for i := range rules {
		if rules[i].Name == "DDLCheckColumnComment" || rules[i].Name == "DDLCheckTableComment" {
			rules[i].Value = "true"
		}
		if rules[i].Name == "DDLCheckColumnNullable" || rules[i].Name == "DDLCheckColumnDefault" {
			rules[i].Value = "true"
		}
		if rules[i].Name == "DDLCheckPrimaryKeyExists" {
			rules[i].Value = "true"
		}
		if rules[i].Name == "DDLTableNamePrefix" {
			rules[i].Value = "tbl_"
		}
	}
	res := CheckTicketWithRules(string(b), "ddl", "postgres", rules)
	if hasRuleID(res, "DDLRequireUpdateTime") {
		t.Fatalf("finance SQL should pass DDLRequireUpdateTime, errs=%s", joinedErrors(res))
	}
}

func joinedErrors(res Result) string {
	var parts []string
	for _, e := range res.Errors {
		if m, _ := e["message"].(string); m != "" {
			parts = append(parts, m)
		}
	}
	for _, s := range res.Statements {
		if s.ErrorLevel == "ERROR" && s.Message != "" {
			parts = append(parts, s.Message)
		}
	}
	return strings.Join(parts, " | ")
}
