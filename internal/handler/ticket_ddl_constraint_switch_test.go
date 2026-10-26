package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"auto-dba/internal/sqlcheck"
	"auto-dba/internal/store"
)

func TestDDLConstraintCheckSwitch(t *testing.T) {
	prevEnabled := ddlConstraintCheckEnabled
	prevRules := reviewRulesForCheck
	t.Cleanup(func() {
		ddlConstraintCheckEnabled = prevEnabled
		reviewRulesForCheck = prevRules
	})
	reviewRulesForCheck = func(engine string) []sqlcheck.Rule {
		return catalogRules(engine)
	}
	h := &TicketHandler{}
	alter := "ALTER TABLE tbl_test ADD COLUMN xx int NOT NULL DEFAULT '0';"

	t.Run("false skips ddl comment check", func(t *testing.T) {
		ddlConstraintCheckEnabled = func(*TicketHandler) bool { return false }
		res := h.checkTicketSQL(alter, "ddl", "postgres", "", "", false)
		if !res.OK || !res.ConstraintCheckSkipped {
			t.Fatalf("expected skip: %+v", res)
		}
		if resultHasRule(res, "DDLCheckColumnComment") {
			t.Fatalf("skipped check should not apply column comment: %+v", res)
		}
		if len(res.Statements) == 0 || !strings.Contains(res.Statements[0].Message, "平台已关闭 DDL 约束检查") {
			t.Fatalf("missing skip message: %+v", res.Statements)
		}
		if res.StatementCount != 1 {
			t.Fatalf("statement count=%d", res.StatementCount)
		}
	})

	t.Run("empty sql still fails", func(t *testing.T) {
		ddlConstraintCheckEnabled = func(*TicketHandler) bool { return false }
		res := h.checkTicketSQL("  ", "ddl", "postgres", "", "", false)
		if res.OK || res.ConstraintCheckSkipped {
			t.Fatalf("empty sql should fail: %+v", res)
		}
	})

	for _, raw := range []string{"", "true", "maybe"} {
		raw := raw
		t.Run("enabled "+raw, func(t *testing.T) {
			ddlConstraintCheckEnabled = func(*TicketHandler) bool {
				return store.ParseDDLConstraintCheckEnabled(raw)
			}
			res := h.checkTicketSQL(alter, "ddl", "postgres", "", "", false)
			if res.OK || res.ConstraintCheckSkipped || !resultHasRule(res, "DDLCheckColumnComment") {
				t.Fatalf("raw %q should fail column comment: %+v", raw, res)
			}
		})
	}

	t.Run("false still checks dml", func(t *testing.T) {
		ddlConstraintCheckEnabled = func(*TicketHandler) bool { return false }
		res := h.checkTicketSQL("DELETE FROM t;", "dml", "postgres", "", "", false)
		if res.OK || res.ConstraintCheckSkipped || !resultHasRule(res, "DMLWhere") {
			t.Fatalf("dml should still fail: %+v", res)
		}
	})
}

func TestStampConstraintCheckSkippedKeepsExtra(t *testing.T) {
	raw := stampConstraintCheckSkipped(`{"shard_db":true,"data_source_ids":["a"]}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m["constraint_check_skipped"] != true || m["shard_db"] != true {
		t.Fatalf("stamp lost fields: %s", raw)
	}
	ids, _ := m["data_source_ids"].([]any)
	if len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("data sources lost: %s", raw)
	}
	if stampConstraintCheckSkipped("not-json") != "not-json" {
		t.Fatal("invalid json should stay unchanged")
	}
}

func TestCheckTicketSQLCommentFollowsEngine(t *testing.T) {
	prevEnabled := ddlConstraintCheckEnabled
	prevRules := reviewRulesForCheck
	t.Cleanup(func() {
		ddlConstraintCheckEnabled = prevEnabled
		reviewRulesForCheck = prevRules
	})
	ddlConstraintCheckEnabled = func(*TicketHandler) bool { return true }
	seen := map[string]int{}
	reviewRulesForCheck = func(engine string) []sqlcheck.Rule {
		seen[engine]++
		return []sqlcheck.Rule{{Name: "DDLCheckColumnComment", Value: "true", Label: "列必须有注释"}}
	}
	sql := `ALTER TABLE public.tbl_lxz_discount_account
ADD COLUMN IF NOT EXISTS corporation_id bigint NOT NULL DEFAULT 0;
COMMENT ON COLUMN public.tbl_lxz_discount_account.corporation_id IS '集团 ID';`
	h := &TicketHandler{}
	// 检测、提交、执行前复核都走 checkTicketSQL，引擎只由 db_type / 实例类型决定。
	pg := h.checkTicketSQL(sql, "ddl", "PostgreSQL", "", "", false)
	if !pg.OK || resultHasRule(pg, "DDLCheckColumnComment") {
		t.Fatalf("PostgreSQL 应认 COMMENT ON COLUMN: %+v", pg)
	}
	sr := h.checkTicketSQL(sql, "ddl", "sr", "", "", false)
	if sr.OK || !resultHasRule(sr, "DDLCheckColumnComment") {
		t.Fatalf("StarRocks 不应把 COMMENT ON COLUMN 当成列注释: %+v", sr)
	}
	if seen["postgres"] == 0 || seen["starrocks"] == 0 || seen["mysql"] != 0 {
		t.Fatalf("engines=%v", seen)
	}
}
