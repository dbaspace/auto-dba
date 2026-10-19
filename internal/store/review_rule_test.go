package store

import (
	"strings"
	"testing"
)

func catalogNames(list []ReviewRule) map[string]string {
	out := map[string]string{}
	for _, it := range list {
		out[strings.ToLower(it.Name)] = it.Value
	}
	return out
}

func TestDefaultTicketReviewRulesEngineIsolation(t *testing.T) {
	pg := catalogNames(DefaultTicketReviewRules("postgresql"))
	sr := catalogNames(DefaultTicketReviewRules("sr"))
	if len(pg) == 0 || len(sr) == 0 {
		t.Fatalf("empty catalog pg=%d sr=%d", len(pg), len(sr))
	}

	pgOnly := []string{
		"DDLCheckPrimaryKeyExists", "DDLForbidDropSchema", "DDLTableNamePrefix",
		"DMLUpdateSetAnd", "DMLAllowMultiTable", "DMLInsertRowLimit",
		"DDLDisallowAddColumnWithDefault", "DDLForbidLockVacuumFull",
		"DDLRequireUpdateTime",
	}
	srOnly := []string{"DDLEnablePrimaryKey", "DDLAddColumnRequireDefault"}
	shared := []string{"DMLWhere", "DMLInsertMustSpecifyColumn", "DMLForbidDDL", "DDLForbidTruncate", "DDLForbidGrant"}

	for _, name := range pgOnly {
		if _, ok := pg[strings.ToLower(name)]; !ok {
			t.Fatalf("postgres 目录缺少 %s", name)
		}
		if _, ok := sr[strings.ToLower(name)]; ok {
			t.Fatalf("starrocks 不应包含 PG 专用规则 %s", name)
		}
	}
	for _, name := range srOnly {
		if _, ok := sr[strings.ToLower(name)]; !ok {
			t.Fatalf("starrocks 目录缺少 %s", name)
		}
		if _, ok := pg[strings.ToLower(name)]; ok {
			t.Fatalf("postgres 不应包含 SR 专用规则 %s", name)
		}
	}
	for _, name := range shared {
		if _, ok := pg[strings.ToLower(name)]; !ok {
			t.Fatalf("postgres 缺少共用规则 %s", name)
		}
		if _, ok := sr[strings.ToLower(name)]; !ok {
			t.Fatalf("starrocks 缺少共用规则 %s", name)
		}
	}
}

func TestDefaultTicketReviewRulesJSONRoundtrip(t *testing.T) {
	raw, err := defaultTicketReviewRulesJSON()
	if err != nil || strings.TrimSpace(raw) == "" {
		t.Fatalf("default json: %v %q", err, raw)
	}
	p, err := parseTicketReviewPartitioned(raw)
	if err != nil || p == nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Postgres) == 0 || len(p.StarRocks) == 0 {
		t.Fatalf("partition empty pg=%d sr=%d", len(p.Postgres), len(p.StarRocks))
	}

	has := func(items []reviewCatalogItem, name string) bool {
		for _, it := range items {
			if strings.EqualFold(it.Name, name) {
				return true
			}
		}
		return false
	}
	if !has(p.Postgres, "DMLForbidTautology") || !has(p.StarRocks, "DMLForbidTautology") {
		t.Fatal("缺少 DMLForbidTautology 开关")
	}
	if !has(p.Postgres, "DMLWhere") || !has(p.StarRocks, "DMLWhere") {
		t.Fatal("DDL/DML 分桶后丢失 DMLWhere")
	}
	if !has(p.Postgres, "DDLForbidLockVacuumFull") {
		t.Fatal("postgres 分桶丢失 DDLForbidLockVacuumFull")
	}
	if has(p.StarRocks, "DDLForbidLockVacuumFull") {
		t.Fatal("starrocks 分桶不应含 DDLForbidLockVacuumFull")
	}
}

func TestRefreshStaleDMLWhereLabel(t *testing.T) {
	items := []reviewCatalogItem{{Name: "DMLWhere", Label: oldDMLWhereLabel, Value: `{"tautologies":["1=1","true"]}`}}
	if !refreshStaleReviewLabels(items) || items[0].Label != "UPDATE/DELETE 必须有 WHERE" {
		t.Fatalf("label=%q", items[0].Label)
	}
	items[0].Label = "自定义 WHERE"
	if refreshStaleReviewLabels(items) {
		t.Fatal("custom label should stay")
	}
}

func TestNormalizeReviewEngineAliases(t *testing.T) {
	cases := map[string]string{
		"postgres": "postgres", "postgresql": "postgres", "pg": "postgres", "PG": "postgres",
		"starrocks": "starrocks", "sr": "starrocks", "SR": "starrocks",
		"mysql": "mysql", "tidb": "mysql",
		"": "", "oracle": "",
	}
	for in, want := range cases {
		if got := NormalizeReviewEngine(in); got != want {
			t.Fatalf("%q => %q want %q", in, got, want)
		}
	}
}
