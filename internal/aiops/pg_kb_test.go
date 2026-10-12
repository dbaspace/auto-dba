package aiops

import (
	"testing"

	"auto-dba/internal/store"
)

func TestPostgresKnowledgeCasesNonEmpty(t *testing.T) {
	list := PostgresKnowledgeCases()
	if len(list) < 20 {
		t.Fatalf("want >=20 pg cases, got %d", len(list))
	}
	for _, c := range list {
		if c.Fingerprint == "" || c.MetricKey == "" || c.Title == "" {
			t.Fatalf("incomplete case %+v", c)
		}
		if c.Engine != "postgres" {
			t.Fatalf("engine=%s", c.Engine)
		}
		if len(c.AlertNames) == 0 {
			t.Fatalf("missing alertnames: %s", c.Fingerprint)
		}
	}
}

func TestKnowledgeMatchPGConnHigh(t *testing.T) {
	var rows []*store.SqlInsightCaseRow
	for _, kc := range AllKnowledgeCases() {
		rows = append(rows, &store.SqlInsightCaseRow{
			Fingerprint: kc.Fingerprint, MetricKey: kc.MetricKey, Title: kc.Title,
			AlertNames: JoinAlertNames(kc.AlertNames),
		})
	}
	a := &store.Alert{Labels: map[string]string{"alertname": "PGConnHigh"}}
	hints := MatchCasesForAlert(rows, a, nil)
	if len(hints) == 0 {
		t.Fatal("expected PGConnHigh matches")
	}
	found := false
	for _, h := range hints {
		if h.MetricKey == "conn_usage_ratio" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("want conn_usage_ratio, got %+v", hints)
	}
}

func TestKnowledgeCasesByEngine(t *testing.T) {
	pg := KnowledgeCasesByEngine("postgres")
	if len(pg) == 0 {
		t.Fatal("empty postgres kb")
	}
	if len(pg) >= len(AllKnowledgeCases()) {
		t.Fatal("postgres should be subset of all")
	}
	for _, eng := range []string{"mysql", "redis", "starrocks", "host"} {
		if len(KnowledgeCasesByEngine(eng)) == 0 {
			t.Fatalf("empty %s kb", eng)
		}
	}
	if len(AllKnowledgeCases()) < 50 {
		t.Fatalf("want rich multi-engine kb, got %d", len(AllKnowledgeCases()))
	}
}
