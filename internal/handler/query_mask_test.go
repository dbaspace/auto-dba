package handler

import (
	"testing"

	"auto-dba/internal/dbprobe"
)

func TestLoadQueryMaskLegacyColumnsStayEnabled(t *testing.T) {
	cfg := loadQueryMask("phone, email；id_card")
	if !cfg.Enabled || len(cfg.Columns) != 3 || cfg.Columns[0] != "phone" || cfg.Columns[2] != "id_card" {
		t.Fatalf("%+v", cfg)
	}
}

func TestLoadQueryMaskDisabledKeepsColumns(t *testing.T) {
	raw := `{"enabled":false,"columns":["phone","email"]}`
	cfg := loadQueryMask(raw)
	if cfg.Enabled || len(cfg.Columns) != 2 {
		t.Fatalf("%+v", cfg)
	}
	res := &dbprobe.QueryResult{Columns: []string{"phone"}, Rows: [][]any{{"13800000000"}}}
	maskQueryResult(raw, res)
	if res.Rows[0][0] != "13800000000" {
		t.Fatalf("disabled still masked: %v", res.Rows[0][0])
	}
}

func TestMaskQueryResultEnabled(t *testing.T) {
	res := &dbprobe.QueryResult{
		Columns: []string{"Phone", "name"},
		Rows:    [][]any{{"138", "ann"}, {nil, "bob"}},
	}
	maskQueryResult(`{"enabled":true,"columns":["phone"]}`, res)
	if res.Rows[0][0] != "***" || res.Rows[0][1] != "ann" || res.Rows[1][0] != nil {
		t.Fatalf("%v", res.Rows)
	}
}

func TestMaskBySourceColumnNotAlias(t *testing.T) {
	cases := []struct {
		sql  string
		col  string
		mask bool
	}{
		{"select mobile as mob from t", "mob", true},
		{"select t.mobile from t", "mobile", true},
		{"select t.mobile as mob from t", "mob", true},
		{"select mob as mobile from t", "mobile", false},
		{"select 1 as mobile", "mobile", false},
		{"select carD as x from t", "x", true},
		{"select t.carD from t", "carD", true},
		{"select Card from t", "Card", true},
	}
	raw := `{"enabled":true,"columns":["mobile","card"]}`
	for _, tc := range cases {
		res := &dbprobe.QueryResult{ValidSQL: tc.sql, Columns: []string{tc.col}, Rows: [][]any{{"v"}}}
		maskQueryResult(raw, res)
		got := res.Rows[0][0] == "***"
		if got != tc.mask {
			t.Fatalf("%s masked=%v", tc.sql, got)
		}
	}
}
