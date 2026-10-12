package aiops

import (
	"testing"

	"auto-dba/internal/store"
)

func TestMetricKeysForAlertByAlertName(t *testing.T) {
	a := &store.Alert{
		Title:  "PostgreSQL 连接数使用率过高",
		Labels: map[string]string{"alertname": "PGConnHigh", "service": "postgres"},
	}
	keys := MetricKeysForAlert(a, nil)
	found := false
	for _, k := range keys {
		if k == "conn_usage_ratio" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("want conn_usage_ratio in %v", keys)
	}
}

func TestMatchCasesForAlertByAlertNames(t *testing.T) {
	cases := []*store.SqlInsightCaseRow{
		{
			Fingerprint: "conn_usage_ratio|warning|above", MetricKey: "conn_usage_ratio",
			Title: "连接使用率偏高", AlertNames: "MySQLConn,PGConnHigh", ConfirmCount: 3,
		},
		{
			Fingerprint: "disk_usage|warning|above", MetricKey: "disk_usage",
			Title: "磁盘偏高", AlertNames: "HostDisk",
		},
	}
	a := &store.Alert{Labels: map[string]string{"alertname": "PGConnHigh"}}
	hints := MatchCasesForAlert(cases, a, nil)
	if len(hints) == 0 {
		t.Fatal("expected case match")
	}
	if hints[0].Fingerprint != "conn_usage_ratio|warning|above" {
		t.Fatalf("got %+v", hints[0])
	}
	if hints[0].MatchReason != "alert_names:PGConnHigh" {
		t.Fatalf("reason=%s", hints[0].MatchReason)
	}
}

func TestSuggestedAlertNamesForMetric(t *testing.T) {
	s := SuggestedAlertNamesForMetric("conn_usage_ratio")
	if !containsAll(ParseAlertNames(s), "MySQLConn", "PGConnHigh") {
		t.Fatalf("got %q", s)
	}
}

func containsAll(list []string, want ...string) bool {
	set := map[string]bool{}
	for _, x := range list {
		set[x] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func TestParseAlertNames(t *testing.T) {
	got := ParseAlertNames(" HostDisk, PGStorage ;disk ")
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}
