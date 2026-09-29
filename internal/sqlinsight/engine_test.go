package sqlinsight

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/awr"
	"auto-dba/internal/store"
)

func TestSqlInsightComputeBaseline(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	b := computeBaseline(vals)
	if b.SampleCount != 10 {
		t.Fatalf("sample_count=%d", b.SampleCount)
	}
	if b.Mean == nil || *b.Mean != 5.5 {
		t.Fatalf("mean=%v", b.Mean)
	}
	if b.P50 == nil || *b.P50 < 5.0 || *b.P50 > 6.0 {
		t.Fatalf("p50=%v", b.P50)
	}
	if b.Min == nil || *b.Min != 1 || b.Max == nil || *b.Max != 10 {
		t.Fatalf("min/max=%v/%v", b.Min, b.Max)
	}
}

func TestSqlInsightCompareThreshold(t *testing.T) {
	warn, crit := 0.8, 0.9
	sev, th := compareThreshold(0.95, siDirAbove, &warn, &crit)
	if sev != siSevCritical || th == nil || *th != 0.9 {
		t.Fatalf("above crit: sev=%s th=%v", sev, th)
	}
	sev, _ = compareThreshold(0.85, siDirAbove, &warn, &crit)
	if sev != siSevWarning {
		t.Fatalf("above warn: sev=%s", sev)
	}
	sev, _ = compareThreshold(0.7, siDirAbove, &warn, &crit)
	if sev != siSevNone {
		t.Fatalf("above none: sev=%s", sev)
	}
	w2, c2 := 0.95, 0.90
	sev, _ = compareThreshold(0.88, siDirBelow, &w2, &c2)
	if sev != siSevCritical {
		t.Fatalf("below crit: sev=%s", sev)
	}
}

func TestSqlInsightPercentile(t *testing.T) {
	sorted := []float64{0, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	p95 := percentile(sorted, 0.95)
	if p95 < 90 || p95 > 100 {
		t.Fatalf("p95=%v", p95)
	}
}

func TestSqlInsightFingerprint(t *testing.T) {
	fp := fingerprint("Cache_Hit_Ratio", "warning", "below")
	if fp != "cache_hit_ratio|warning|below" {
		t.Fatalf("fp=%s", fp)
	}
}

func TestSqlInsightBuiltinCatalog(t *testing.T) {
	cat := builtinCatalog()
	if len(cat) < 10 {
		t.Fatalf("catalog too small: %d", len(cat))
	}
	if builtinByKey("cache_hit_ratio") == nil {
		t.Fatal("missing cache_hit_ratio")
	}
	if builtinByKey("no_such") != nil {
		t.Fatal("unexpected key")
	}
}

func TestSqlInsightFindingFromAWRAnomaly(t *testing.T) {
	day := time.Date(2026, 8, 7, 0, 0, 0, 0, time.Local)
	rep := &AWRReportView{Host: "pg.bj.restosuite.shop", Port: 5432, InstanceID: "i1", InstanceName: "ai-beijing-postgres"}
	v, th := 0.9114, 0.95
	a := awr.AwrAnomaly{
		Key: "cache_hit_ratio", Title: "缓存命中率", Severity: "warning", Kind: "threshold",
		Value: &v, Threshold: &th, Unit: "ratio", Message: "缓存命中率低于阈值 0.95",
	}
	f := findingFromAWRAnomaly("run-1", day, rep, a)
	if f == nil {
		t.Fatal("expected finding")
	}
	if f.RuleKind != siRuleKindAWRImport || f.MetricKey != "cache_hit_ratio" || f.Severity != "warning" {
		t.Fatalf("unexpected finding: %+v", f)
	}
	if f.Fingerprint != "cache_hit_ratio|warning|below" {
		t.Fatalf("fingerprint=%s", f.Fingerprint)
	}
}

func TestSqlInsightDedupeFindings(t *testing.T) {
	v := 0.9
	a := &store.SqlInsightFindingRow{MetricKey: "cache_hit_ratio", Severity: "warning", Title: "AWR", Value: &v, RuleKind: siRuleKindAWRImport}
	b := &store.SqlInsightFindingRow{MetricKey: "cache_hit_ratio", Severity: "warning", Title: "Rule", Value: &v, RuleKind: siRuleThreshold}
	c := &store.SqlInsightFindingRow{MetricKey: "tps", Severity: "warning", Title: "TPS", RuleKind: siRuleKindAWRImport}
	out := dedupeFindings([]*store.SqlInsightFindingRow{a, b, c})
	if len(out) != 2 {
		t.Fatalf("want 2 after dedupe, got %d", len(out))
	}
	if out[0].Title != "AWR" {
		t.Fatalf("should keep first (AWR import), got %s", out[0].Title)
	}
}

func TestSqlInsightNormalizeScenario(t *testing.T) {
	sc := normalizeScenario("今晚大促预热，流量上升", []string{"promo", "unknown_tag"})
	if !sc.has(siTagPromo) || !sc.has(siTagExpectedLoadUp) {
		t.Fatalf("tags=%v", sc.Tags)
	}
	for _, ttag := range sc.Tags {
		if ttag == "unknown_tag" {
			t.Fatal("unknown tag should be dropped")
		}
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestSqlInsightDiagnoseConnIdleHeavy(t *testing.T) {
	rep := &AWRReportView{}
	sec := &awr.AwrSections{
		Sections: []*awr.AwrSection{{
			Name: "connections",
			Metrics: []*awr.AwrMetric{
				{Key: "conn_usage_ratio", Max: floatPtr(0.92)},
				{Key: "backends", Avg: floatPtr(184), Max: floatPtr(200)},
				{Key: "idle_conns", Avg: floatPtr(130)},
				{Key: "active_conns", Max: floatPtr(40)},
			},
		}},
		LongTxn: []*awr.AwrTopSQL{{QueryHash: "x"}},
	}
	b, _ := json.Marshal(sec)
	rep.Sections = b
	f := &store.SqlInsightFindingRow{MetricKey: "conn_usage_ratio", Severity: siSevWarning, Title: "连接使用率", Value: floatPtr(0.92)}
	enrichDeepAnalysis(f, rep, nil)
	deep := deepFromEvidence(f.Evidence)
	if deep == nil {
		t.Fatal("expected deep_analysis")
	}
	hasIdle, hasTool, hasParam := false, false, false
	for _, d := range deep.Diagnoses {
		if d.Code == "idle_heavy" {
			hasIdle = true
		}
	}
	for _, r := range deep.Recommendations {
		if r.Category == "tool" {
			hasTool = true
		}
		if r.Category == "parameter" {
			hasParam = true
		}
	}
	if !hasIdle || !hasTool || !hasParam {
		t.Fatalf("idle=%v tool=%v param=%v deep=%+v", hasIdle, hasTool, hasParam, deep)
	}
	if !strings.Contains(f.Suggestion, "空闲") && !strings.Contains(f.Suggestion, "连接池") {
		t.Fatalf("suggestion=%s", f.Suggestion)
	}
}

func TestSqlInsightDiagnoseConnSelfNoIdle(t *testing.T) {
	rep := &AWRReportView{}
	sec := &awr.AwrSections{Sections: []*awr.AwrSection{{
		Name: "connections",
		Metrics: []*awr.AwrMetric{
			{Key: "conn_usage_ratio", Max: floatPtr(0.88)},
			{Key: "backends", Avg: floatPtr(176), Max: floatPtr(200)},
		},
	}}}
	b, _ := json.Marshal(sec)
	rep.Sections = b
	f := &store.SqlInsightFindingRow{MetricKey: "conn_usage_ratio", Severity: siSevWarning, Value: floatPtr(0.88)}
	enrichDeepAnalysis(f, rep, nil)
	deep := deepFromEvidence(f.Evidence)
	if deep == nil || deep.Summary == "" {
		t.Fatal("expected deep analysis without idle")
	}
	hasNear := false
	for _, d := range deep.Diagnoses {
		if d.Code == "near_cap" {
			hasNear = true
		}
		if d.Code == "idle_heavy" {
			t.Fatal("should not force idle_heavy without idle data")
		}
	}
	if !hasNear {
		t.Fatalf("want near_cap, got %+v", deep.Diagnoses)
	}
}

func TestSqlInsightDiagnoseConnPromoKeepsIdleHeavy(t *testing.T) {
	rep := &AWRReportView{}
	sec := &awr.AwrSections{Sections: []*awr.AwrSection{{
		Metrics: []*awr.AwrMetric{
			{Key: "conn_usage_ratio", Max: floatPtr(0.93)},
			{Key: "backends", Avg: floatPtr(190), Max: floatPtr(200)},
			{Key: "idle_conns", Avg: floatPtr(150)},
			{Key: "active_conns", Max: floatPtr(30)},
		},
	}}}
	b, _ := json.Marshal(sec)
	rep.Sections = b
	f := &store.SqlInsightFindingRow{MetricKey: "conn_usage_ratio", Severity: siSevCritical, Value: floatPtr(0.93)}
	sc := normalizeScenario("大促", []string{siTagPromo})
	enrichDeepAnalysis(f, rep, sc)
	deep := deepFromEvidence(f.Evidence)
	hasIdle, hasScenario := false, false
	for _, d := range deep.Diagnoses {
		if d.Code == "idle_heavy" {
			hasIdle = true
		}
		if d.Code == "scenario_explained" {
			hasScenario = true
		}
	}
	if !hasIdle || !hasScenario {
		t.Fatalf("idle=%v scenario=%v", hasIdle, hasScenario)
	}
	if deep.ScenarioApplied == nil || len(deep.ScenarioApplied.Effects) == 0 {
		t.Fatal("expected scenario_applied.effects")
	}
}

func TestSqlInsightDiagnoseTPSExpectedDown(t *testing.T) {
	f := &store.SqlInsightFindingRow{MetricKey: "tps", Severity: siSevWarning, Value: floatPtr(10)}
	rep := &AWRReportView{}
	sec := &awr.AwrSections{Sections: []*awr.AwrSection{{
		Metrics: []*awr.AwrMetric{{Key: "tps", Avg: floatPtr(10)}},
	}}}
	b, _ := json.Marshal(sec)
	rep.Sections = b
	sc := normalizeScenario("业务流量下降属预期", []string{siTagExpectedLoadDown})
	enrichDeepAnalysis(f, rep, sc)
	deep := deepFromEvidence(f.Evidence)
	if deep == nil || deep.ScenarioApplied == nil {
		t.Fatal("expected scenario")
	}
	found := false
	for _, e := range deep.ScenarioApplied.Effects {
		if e == "tps_drop_matches_expected_load_down" {
			found = true
		}
	}
	if !found {
		t.Fatalf("effects=%v", deep.ScenarioApplied.Effects)
	}
}

func TestSqlInsightDiagnoseCacheHit(t *testing.T) {
	f := &store.SqlInsightFindingRow{MetricKey: "cache_hit_ratio", Severity: siSevWarning, Value: floatPtr(0.91)}
	rep := &AWRReportView{}
	sec := &awr.AwrSections{Sections: []*awr.AwrSection{{
		Metrics: []*awr.AwrMetric{{Key: "cache_hit_ratio", Min: floatPtr(0.91)}},
	}}}
	b, _ := json.Marshal(sec)
	rep.Sections = b
	enrichDeepAnalysis(f, rep, nil)
	deep := deepFromEvidence(f.Evidence)
	if deep == nil || len(deep.Recommendations) == 0 {
		t.Fatal("expected cache hit recommendations")
	}
}

