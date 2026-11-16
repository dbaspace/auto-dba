package instoverview

import (
	"math"
	"strings"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func TestPGStorageFillsWithinTwoWeeks(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	item := AnalyzePGInstance(PGInstanceInput{
		Days: 7, Capacity: 100 * gib,
		SizeEnd: f64(50 * gib), SizeStart: f64(10 * gib),
	})
	if item.Storage.Level != "danger" {
		t.Fatalf("level %s advice %s", item.Storage.Level, item.Storage.Advice)
	}
	if item.Storage.DaysToFull == nil || *item.Storage.DaysToFull > 14 {
		t.Fatalf("days %+v", item.Storage.DaysToFull)
	}
	if !strings.Contains(item.Advice, "两周内可能写满") {
		t.Fatalf("advice %s", item.Advice)
	}
}

func TestPGStorageMissingSamplesAndCapacity(t *testing.T) {
	noEnd := AnalyzePGInstance(PGInstanceInput{Days: 7, Capacity: 100})
	if noEnd.Storage.Advice != "没有库大小样本" {
		t.Fatalf("end %s", noEnd.Storage.Advice)
	}
	noStart := AnalyzePGInstance(PGInstanceInput{Days: 7, Capacity: 100 << 30, SizeEnd: f64(10)})
	if noStart.Storage.HasPrev || !strings.Contains(noStart.Storage.Advice, "没有 7 天前的样本") {
		t.Fatalf("start %+v", noStart.Storage)
	}
	if noStart.Storage.DaysToFull != nil {
		t.Fatal("missing start should not estimate days")
	}
	noCap := AnalyzePGInstance(PGInstanceInput{Days: 7, SizeEnd: f64(200), SizeStart: f64(100)})
	if noCap.Storage.CapacityKnown || noCap.Storage.DaysToFull != nil {
		t.Fatalf("capacity %+v", noCap.Storage)
	}
	if noCap.Storage.GrowthBytes != 100 || noCap.Storage.Advice != "没有购买容量，不能估算写满天数" {
		t.Fatalf("growth %+v", noCap.Storage)
	}
}

func TestPGLoadAdvice(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	end := start.Add(7 * 24 * time.Hour)
	mid := start.Add(end.Sub(start) / 2)
	item := AnalyzePGInstance(PGInstanceInput{
		Days: 7, Start: start, End: end, Capacity: 100 << 30,
		SizeEnd: f64(10), SizeStart: f64(10),
		Deadlocks: []PGPoint{{T: start.Unix(), V: 0.2}},
		CPU: []PGPoint{
			{T: start.Unix(), V: 0.4},
			{T: mid.Unix(), V: 0.9},
		},
		TPS: []PGPoint{
			{T: start.Unix(), V: 100},
			{T: mid.Unix(), V: 40},
		},
	})
	if item.Deadlocks.Level != "danger" || !strings.Contains(item.Advice, "有死锁") {
		t.Fatalf("deadlock %+v advice %s", item.Deadlocks, item.Advice)
	}
	if item.CPU.Level != "warn" || !strings.Contains(item.CPU.Advice, "后半段升高") {
		t.Fatalf("cpu %+v", item.CPU)
	}
	if item.TPS.Level != "warn" || item.TPS.Advice != "吞吐下降" {
		t.Fatalf("tps %+v", item.TPS)
	}
	if item.QPS.Reason != "没有语句统计" {
		t.Fatalf("qps %+v", item.QPS)
	}
	if item.Locks.Known {
		t.Fatal("locks should be missing")
	}
}

func TestPGStorageUsesCurrentRate(t *testing.T) {
	cur := 63.0
	item := AnalyzePGInstance(PGInstanceInput{
		Days: 1, Capacity: 100, SizeEnd: f64(57), SizeStart: f64(50), CurrentRate: &cur,
	})
	if !item.Storage.RateCurrent || item.Storage.Rate != 63 {
		t.Fatalf("rate %+v", item.Storage)
	}
	if item.Storage.GrowthBytes != 7 {
		t.Fatalf("growth %+v", item.Storage)
	}
}

func TestPGCPUStatUsesFinePeak(t *testing.T) {
	item := AnalyzePGInstance(PGInstanceInput{
		CPU: []PGPoint{{T: 1, V: 0.59}},
		CPUStat: &PGRateStat{Known: true, Avg: 0.16, Max: 1.14},
	})
	if item.CPU.Max != 1.14 || item.CPU.Level != "danger" {
		t.Fatalf("cpu %+v", item.CPU)
	}
}

func TestSortPGItemsByFullThenCPU(t *testing.T) {
	soon, later, none := 10.0, 40.0, math.Inf(1)
	items := []PGTrendItem{
		{ID: "later", Storage: &PGStorage{DiskTrend: DiskTrend{DaysToFull: &later}}, CPU: &PGMetric{Known: true, Max: 0.99}},
		{ID: "none", CPU: &PGMetric{Known: true, Max: 0.5}},
		{ID: "soon", Storage: &PGStorage{DiskTrend: DiskTrend{DaysToFull: &soon}}, CPU: &PGMetric{Known: true, Max: 0.2}},
	}
	_ = none
	SortPGItems(items)
	if items[0].ID != "soon" || items[1].ID != "later" || items[2].ID != "none" {
		t.Fatalf("order %s %s %s", items[0].ID, items[1].ID, items[2].ID)
	}
	sum := SummarizePG([]PGTrendItem{
		{Level: "danger", Storage: &PGStorage{CapacityKnown: true, DiskTrend: DiskTrend{DaysToFull: &soon, Rate: 90, HasPrev: true, GrowthBytes: 1 << 40, SizeBytes: 1 << 40}}, Deadlocks: &PGMetric{Known: true, Level: "danger"}, CPU: &PGMetric{Known: true, Level: "warn"}},
	}, 7)
	if sum.Danger != 1 || sum.FullRisk != 1 || sum.Deadlocks != 1 || sum.CPUHigh != 1 {
		t.Fatalf("summary %+v", sum)
	}
	if len(sum.Conclusions) < 3 {
		t.Fatalf("conclusions %+v", sum.Conclusions)
	}
}

func TestPGQueryStepAndSelector(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	if PGQueryStep(start, start.Add(48*time.Hour)) != 5*time.Minute {
		t.Fatal("short window")
	}
	if PGQueryStep(start, start.Add(7*24*time.Hour)) != 15*time.Minute {
		t.Fatal("week window")
	}
	sel := PGInstanceSelector([]string{"10.0.0.1", "pg.test.example", "10.0.0.1", ""})
	want := `instance=~"(10\\.0\\.0\\.1|pg\\.test\\.example)(:[0-9]+)?"`
	if sel != want {
		t.Fatalf("selector %s", sel)
	}
	if PGInstanceSelector(nil) != "" || PGSizeExpr("") != "" {
		t.Fatal("empty selector should skip the query")
	}
	name := PGNameSelector([]string{"billing", "pg.test.example"})
	if name != `d_b_instance_name=~"billing|pg\\.test\\.example"` {
		t.Fatalf("name %s", name)
	}
	hostSel := PGHostnameExactSelector([]string{"test-pg", "10.100.112.10"})
	if hostSel != `hostname=~"10\\.100\\.112\\.10|test-pg"` {
		t.Fatalf("hostname %s", hostSel)
	}
	expr := PGTPSByHostname([]string{"test-pg"})
	wantTPS := `sum by (hostname) (rate(pg_stat_database_xact_commit{hostname=~"test-pg"}[5m])) + sum by (hostname) (rate(pg_stat_database_xact_rollback{hostname=~"test-pg"}[5m]))`
	if expr != wantTPS {
		t.Fatalf("tps %s", expr)
	}
	qps := PGQPSByHostname([]string{"test-pg"})
	if qps != `sum by (hostname) (rate(pg_stat_statements_calls_total{hostname=~"test-pg"}[5m]))` {
		t.Fatalf("qps %s", qps)
	}
	over := PGOverTime(qps, "max_over_time", start, start.Add(24*time.Hour))
	if over != `max_over_time((`+qps+`)[86400s:60s])` {
		t.Fatalf("over %s", over)
	}
	cands := PGHostnameCandidates("test-pg", "db.example", "10.100.112.10")
	picked := PickPGHostnames([]PGHostCandidate{{ID: "a", Names: cands}}, map[string]bool{"10.100.112.10": true, "test-pg": true})
	if picked["a"] != "test-pg" {
		t.Fatalf("pick %+v", picked)
	}
	dom := PGDomainSelector([]string{"pg.test.example"})
	if dom != `domain=~"(pg\\.test\\.example)(:[0-9]+)?"` {
		t.Fatalf("domain %s", dom)
	}
	joined := PGEither(sel, dom, PGSizeExpr)
	if !strings.Contains(joined, sel) || !strings.Contains(joined, " or ") || !strings.Contains(joined, dom) {
		t.Fatalf("either %s", joined)
	}
}

func TestPGLookupSeriesThenDomain(t *testing.T) {
	var idx PGIndex
	idx.Add("a", "10.0.0.1", "db.example", 5432)
	if id, ok := idx.LookupSeries("10.9.9.9:9628", "db.example", ""); !ok || id != "a" {
		t.Fatalf("domain fallback %s %v", id, ok)
	}
	if id, ok := idx.LookupSeries("", "", "db.example"); !ok || id != "a" {
		t.Fatalf("hostname %s %v", id, ok)
	}
	if id, ok := idx.LookupSeries("10.0.0.1:5432", "other", ""); !ok || id != "a" {
		t.Fatalf("instance first %s %v", id, ok)
	}
}

func TestPGInactiveSlots(t *testing.T) {
	active, total, maxSlots := 1.0, 3.0, 8.0
	item := AnalyzePGInstance(PGInstanceInput{
		Days: 7, Capacity: 100, SizeEnd: f64(10), SizeStart: f64(10),
		SlotActive: &active, SlotTotal: &total, SlotMax: &maxSlots,
	})
	if item.Slots == nil || !item.Slots.Known || item.Slots.Advice != "有未激活复制槽，确认是否可删" {
		t.Fatalf("slots %+v", item.Slots)
	}
	if item.Slots.Max == nil || *item.Slots.Max != 8 {
		t.Fatalf("max %+v", item.Slots.Max)
	}
	sum := SummarizePG([]PGTrendItem{item}, 7)
	found := false
	for _, c := range sum.Conclusions {
		if c == "1 台有未激活复制槽，确认是否可删" {
			found = true
		}
	}
	if !found {
		t.Fatalf("conclusions %v", sum.Conclusions)
	}
}

func TestPGTimeoutReason(t *testing.T) {
	item := AnalyzePGInstance(PGInstanceInput{
		Days: 7, SizeEndTimeout: true, QPSTimeout: true, TPSTimeout: true,
	})
	if item.Storage.Advice != "库大小查询超时" {
		t.Fatalf("storage %s", item.Storage.Advice)
	}
	if item.QPS.Reason != "查询超时" || item.TPS.Reason != "查询超时" {
		t.Fatalf("qps %s tps %s", item.QPS.Reason, item.TPS.Reason)
	}
	if item.Locks.Reason != "没有样本" {
		t.Fatalf("locks %s", item.Locks.Reason)
	}
	sum := SummarizePG([]PGTrendItem{item}, 7)
	found := false
	for _, c := range sum.Conclusions {
		if c == "有指标查询超时，报告只含已返回的数据" {
			found = true
		}
		if strings.Contains(c, "没有库大小样本") {
			t.Fatalf("timeout should not be counted as missing sample: %v", sum.Conclusions)
		}
	}
	if !found {
		t.Fatalf("conclusions %v", sum.Conclusions)
	}
}

func TestPGIndexMatchesExporterPortAndNames(t *testing.T) {
	var idx PGIndex
	idx.Add("a", "10.0.0.1", "db.example", 5432)
	idx.AddName("a", "billing", "prod")
	if id, ok := idx.Lookup("10.0.0.1:5432"); !ok || id != "a" {
		t.Fatalf("db port %s %v", id, ok)
	}
	if id, ok := idx.Lookup("10.0.0.1:9187"); !ok || id != "a" {
		t.Fatalf("exporter %s %v", id, ok)
	}
	if id, ok := idx.Lookup("db.example:5432"); !ok || id != "a" {
		t.Fatalf("domain %s %v", id, ok)
	}
	idx.Add("b", "10.0.0.1", "other.example", 5433)
	if _, ok := idx.Lookup("10.0.0.1:9187"); ok {
		t.Fatal("shared host exporter port should stay unmatched")
	}
	if id, ok := idx.Lookup("10.0.0.1:5433"); !ok || id != "b" {
		t.Fatalf("second port %s %v", id, ok)
	}
	idx.AddName("b", "billing", "test")
	if id, ok := idx.LookupName("billing", "test"); !ok || id != "b" {
		t.Fatalf("name %s %v", id, ok)
	}
	if _, ok := idx.LookupName("billing", ""); ok {
		t.Fatal("ambiguous name without idc")
	}
}
