package store

import (
	"strings"
	"testing"
)

func TestNormalizeAlertTime(t *testing.T) {
	cases := map[string]string{
		"2026-10-18T01:01:00.000Z":      "2026-10-18T01:01:00Z",
		"2026-10-18T09:06:00.000+08:00": "2026-10-18T01:06:00Z",
		"":                              "",
	}
	for in, want := range cases {
		if got := normalizeAlertTime(in); got != want {
			t.Fatalf("normalize(%q)=%q want %q", in, got, want)
		}
	}
}

func TestAlertHistoryFromArgsOrder(t *testing.T) {
	from, args := alertHistoryFrom(AlertHistoryQuery{
		AlertFilter: AlertFilter{
			Since: "2026-10-11T04:00:00.000Z", Until: "2026-10-18T04:00:00.000Z", TenantID: "t1", Severity: "P1", Keyword: "Disk",
		},
		AlertID: "a1",
	})
	if strings.Count(from, "?") != len(args) {
		t.Fatalf("placeholders %d != args %d: %s", strings.Count(from, "?"), len(args), from)
	}
	for _, want := range []string{"ROW_NUMBER() OVER (PARTITION BY e.alert_id, e.action", "NOT LIKE '%resolve%'", "w.created_at >= ?", "ev.created_at <= ?", "a.id=?"} {
		if !strings.Contains(from, want) {
			t.Fatalf("query missing %q: %s", want, from)
		}
	}
	want := []any{"2026-10-11T04:00:00Z", "2026-10-18T04:00:00Z", "2026-10-11T04:00:00Z", "2026-10-18T04:00:00Z", "t1", "P1", "a1", "%disk%"}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("arg[%d]=%v want %v (all %#v)", i, args[i], want[i], args)
		}
	}
}

func TestAlertHistoryLimitOffset(t *testing.T) {
	if l, o := (AlertHistoryQuery{}).limitOffset(); l != 20 || o != 0 {
		t.Fatalf("default %d %d", l, o)
	}
	if l, o := (AlertHistoryQuery{Page: 3, PageSize: 50}).limitOffset(); l != 50 || o != 100 {
		t.Fatalf("page3 %d %d", l, o)
	}
	if l, _ := (AlertHistoryQuery{PageSize: 10000}).limitOffset(); l != 200 {
		t.Fatalf("cap %d", l)
	}
}
