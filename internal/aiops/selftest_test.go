package aiops

import (
	"strings"
	"testing"

	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

// 功能自测：知识库 ↔ 种子 alertname 全覆盖对齐。
func TestSelfPGAlertNamesHaveKnowledge(t *testing.T) {
	alertNames := []string{
		"PGConnHigh", "PGStorage", "PGCacheHitLow", "PGReplicationLag",
		"PGDeadlocks", "PGLongTransaction", "PGSlowQuery", "PGTPSDrop", "PGActiveSessions",
	}
	var rows []*store.SqlInsightCaseRow
	for _, kc := range AllKnowledgeCases() {
		rows = append(rows, &store.SqlInsightCaseRow{
			Fingerprint: kc.Fingerprint, MetricKey: kc.MetricKey, Title: kc.Title,
			AlertNames: JoinAlertNames(kc.AlertNames), Severity: kc.Severity,
			RootCauseTemplate: kc.RootCauseTemplate, ActionTemplate: kc.ActionTemplate,
		})
	}
	for _, an := range alertNames {
		a := &store.Alert{Labels: map[string]string{"alertname": an}, Service: "postgres"}
		hints := MatchCasesForAlert(rows, a, nil)
		if len(hints) == 0 {
			t.Fatalf("alertname=%s 无匹配知识库案例", an)
		}
		if strings.TrimSpace(hints[0].Title) == "" || strings.TrimSpace(hints[0].ActionTemplate) == "" {
			t.Fatalf("alertname=%s 案例文案不完整: %+v", an, hints[0])
		}
	}
}

func TestSelfNotifyPayloadContextFields(t *testing.T) {
	p := notify.AlertPayload{
		Severity: "P1", Event: "trigger", Title: "PostgreSQL 连接数使用率过高",
		Service: "postgres", Cluster: "bj", CurValue: "0.91", Threshold: "0.8", Handle: "unclaimed",
		InstanceID: "inst-1", ImpactSummary: "影响下游=2 上游=0",
		ContextSummary: "影响下游=2 · 案例:PG 连接使用率偏高 · 实例:pg-demo",
		CaseHints: []notify.AlertCaseHint{{Fingerprint: "conn_usage_ratio|warning|above", MetricKey: "conn_usage_ratio", Title: "PG 连接使用率偏高"}},
	}
	text := notify.FormatAlertText(p)
	if !strings.Contains(text, "案例:PG 连接使用率偏高") {
		t.Fatalf("text missing context: %s", text)
	}
	md := notify.FormatAlertMarkdown(p)
	if !strings.Contains(md, "诊断:") {
		t.Fatalf("markdown missing 诊断: %s", md)
	}
}

func TestSelfDefaultAlertRulesHaveTenantReadySeed(t *testing.T) {
	rules := store.DefaultAlertRules()
	seen := map[string]bool{}
	for _, r := range rules {
		an := r.Labels["alertname"]
		if strings.HasPrefix(an, "PG") {
			seen[an] = true
		}
		if r.ID == "" || r.CreatedBy != "system" {
			t.Fatalf("bad seed rule %+v", r)
		}
	}
	for _, want := range []string{"PGConnHigh", "PGStorage", "PGCacheHitLow", "PGReplicationLag", "PGDeadlocks"} {
		if !seen[want] {
			t.Fatalf("missing seed rule alertname=%s", want)
		}
	}
}

func TestSelfResolveTargetFromLabels(t *testing.T) {
	h, p := splitHostPort("10.0.0.8:5432")
	if h != "10.0.0.8" || p != 5432 {
		t.Fatalf("got %s:%d", h, p)
	}
	if AlertNameFromLabels(map[string]string{"alertname": "PGCacheHitLow"}) != "PGCacheHitLow" {
		t.Fatal("alertname miss")
	}
	keys := MetricKeysForAlert(&store.Alert{
		Title: "PostgreSQL 缓存命中率过低",
		Labels: map[string]string{"alertname": "PGCacheHitLow"},
	}, nil)
	found := false
	for _, k := range keys {
		if k == "cache_hit_ratio" {
			found = true
		}
	}
	if !found {
		t.Fatalf("keys=%v", keys)
	}
}

func TestSelfBufferHitTitleHeuristic(t *testing.T) {
	// 种子规则文案用「缓冲命中率」，须命中 cache_hit_ratio
	keys := MetricKeysForAlert(&store.Alert{Title: "PostgreSQL 缓冲命中率过低", Service: "postgres"}, nil)
	found := false
	for _, k := range keys {
		if k == "cache_hit_ratio" {
			found = true
		}
	}
	if !found {
		t.Fatalf("keys=%v", keys)
	}
	hints := MatchCasesForAlert(knowledgeCaseRows(), &store.Alert{Title: "PostgreSQL 缓冲命中率过低"}, nil)
	if len(hints) == 0 {
		t.Fatal("expected kb match via title heuristic")
	}
}
