package sqlcheck

import (
	"strings"
	"testing"
)

func TestOrderFunctionsBeforeTriggersMovesLaterDef(t *testing.T) {
	sql := `
CREATE TABLE public.tbl_agent_message (id int);
COMMENT ON TABLE public.tbl_agent_message IS 'messages';
CREATE INDEX idx_agent_message_session ON public.tbl_agent_message (id);
CREATE TRIGGER trg_tbl_agent_message_modify_time
BEFORE UPDATE ON public.tbl_agent_message
FOR EACH ROW EXECUTE FUNCTION update_modify_time();
CREATE OR REPLACE FUNCTION update_modify_time()
RETURNS trigger AS $$
BEGIN
  NEW.modify_time = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
`
	got := OrderFunctionsBeforeTriggers(SplitSQL(sql))
	if len(got) != 5 {
		t.Fatalf("stmts=%d want 5: %+v", len(got), got)
	}
	if !strings.Contains(got[0], "CREATE TABLE") {
		t.Fatalf("建表应留在最前: %s", got[0])
	}
	if !strings.Contains(got[1], "COMMENT ON") || !strings.Contains(got[2], "CREATE INDEX") {
		t.Fatalf("注释和索引顺序被打乱: %+v", got)
	}
	if !strings.Contains(strings.ToUpper(got[3]), "CREATE OR REPLACE FUNCTION") {
		t.Fatalf("函数应在触发器前: %s", got[3])
	}
	if !strings.Contains(strings.ToUpper(got[4]), "CREATE TRIGGER") {
		t.Fatalf("触发器应在函数后: %s", got[4])
	}
}

func TestOrderFunctionsBeforeTriggersKeepsEarlierDef(t *testing.T) {
	stmts := []string{
		"CREATE FUNCTION update_modify_time() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$",
		"CREATE TABLE t (id int)",
		"CREATE TRIGGER trg BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION update_modify_time()",
		"CREATE OR REPLACE FUNCTION update_modify_time() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$",
	}
	got := OrderFunctionsBeforeTriggers(stmts)
	if len(got) != len(stmts) {
		t.Fatalf("len=%d", len(got))
	}
	for i := range stmts {
		if got[i] != stmts[i] {
			t.Fatalf("已有前置定义时不应重排 i=%d\n got %s\nwant %s", i, got[i], stmts[i])
		}
	}
}

func TestOrderFunctionsBeforeTriggersMatchesSchema(t *testing.T) {
	stmts := []string{
		"CREATE TABLE t (id int)",
		`CREATE TRIGGER trg BEFORE UPDATE ON t FOR EACH ROW EXECUTE PROCEDURE public.update_modify_time()`,
		`CREATE FUNCTION "public"."update_modify_time"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`,
	}
	got := OrderFunctionsBeforeTriggers(stmts)
	if !strings.Contains(got[1], "CREATE FUNCTION") || !strings.Contains(got[2], "CREATE TRIGGER") {
		t.Fatalf("schema 与无 schema 应视为同一函数: %+v", got)
	}
}

func TestOrderFunctionsBeforeTriggersLeavesUnknown(t *testing.T) {
	stmts := []string{
		"CREATE TABLE t (id int)",
		"CREATE TRIGGER trg BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION other_fn()",
		"CREATE FUNCTION update_modify_time() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$",
	}
	got := OrderFunctionsBeforeTriggers(stmts)
	for i := range stmts {
		if got[i] != stmts[i] {
			t.Fatalf("无关函数不应移动 i=%d", i)
		}
	}
}

func TestOrderFunctionsBeforeTriggersSameSpotKeepsOrder(t *testing.T) {
	stmts := []string{
		"CREATE TRIGGER trg BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION fn_b() EXECUTE FUNCTION fn_a()",
		"CREATE FUNCTION fn_a() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$",
		"CREATE FUNCTION fn_b() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$",
	}
	got := OrderFunctionsBeforeTriggers(stmts)
	if len(got) != 3 || !strings.Contains(got[0], "fn_a") || !strings.Contains(got[1], "fn_b") || !strings.Contains(got[2], "CREATE TRIGGER") {
		t.Fatalf("同一触发器前应按原文顺序插入: %+v", got)
	}
}

func TestOrderFunctionsBeforeTriggersSeparateUses(t *testing.T) {
	stmts := []string{
		"CREATE TRIGGER trg_a BEFORE UPDATE ON ta FOR EACH ROW EXECUTE FUNCTION fn_a()",
		"CREATE TRIGGER trg_b BEFORE UPDATE ON tb FOR EACH ROW EXECUTE FUNCTION fn_b()",
		"CREATE FUNCTION fn_b() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$",
		"CREATE FUNCTION fn_a() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$",
	}
	got := OrderFunctionsBeforeTriggers(stmts)
	want := []string{"fn_a", "trg_a", "fn_b", "trg_b"}
	if len(got) != len(want) {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	for i, key := range want {
		if !strings.Contains(got[i], key) {
			t.Fatalf("i=%d 应含 %s，实际 %s", i, key, got[i])
		}
	}
}
