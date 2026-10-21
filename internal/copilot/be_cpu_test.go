package copilot

import (
	"context"
	"strings"
	"testing"

	"auto-dba/internal/aiops"
	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

func TestPromSRBECPUTemplateEscapesHost(t *testing.T) {
	tpl, err := findTemplate("prom.sr.be_cpu")
	if err != nil {
		t.Fatal(err)
	}
	if !tpl.Range || tpl.SourceRef != sourcePromBase {
		t.Fatalf("template=%+v", tpl)
	}
	q, err := renderTemplate(tpl, map[string]string{
		"host":        promHostRegex("dorisdb.bj.restosuite.cn"),
		"range_hours": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `instance=~"dorisdb[.]bj[.]restosuite[.]cn.*"`) || strings.Contains(q, `\.`) {
		t.Fatal(q)
	}
	if strings.Contains(q, "query.universe") {
		t.Fatal("基址不能写进 PromQL")
	}
}

func TestValidatePromHost(t *testing.T) {
	if err := validatePromHost("dorisdb.bj.restosuite.cn"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "a b", `foo{bar}`, "up or 1", "host;drop"} {
		if validatePromHost(bad) == nil {
			t.Fatalf("want reject %q", bad)
		}
	}
}

func TestRangeHoursCap(t *testing.T) {
	if rangeHours("") != 1 || rangeHours("8") != 6 || rangeHours("2") != 2 {
		t.Fatalf("hours %d %d %d", rangeHours(""), rangeHours("8"), rangeHours("2"))
	}
	if utteranceHours("最近1小时") != 1 || utteranceHours("最近8小时") != 6 || utteranceHours("") != 1 {
		t.Fatal(utteranceHours("最近1小时"), utteranceHours("最近8小时"))
	}
}

func TestSummarizeRange(t *testing.T) {
	got := summarizeRange([]promclient.Sample{{
		Metric: map[string]string{"instance": "be-1:8040"},
		Values: [][]any{{1, "10"}, {2, "30"}, {3, "20"}},
	}})
	for _, want := range []string{"be-1:8040", "min=10.0", "avg=20.0", "max=30.0"} {
		if !strings.Contains(got, want) {
			t.Fatal(got)
		}
	}
}

func TestWantsBECPUScreenshot(t *testing.T) {
	q := "分析下北京环境 dorisdb.bj.restosuite.cn 集群 最近1小时 be 节点的cpu使用情况"
	if !wantsBECPU(q) {
		t.Fatal("want be cpu")
	}
	u := aiops.ParseUtterance(q)
	if u.Object != "dorisdb.bj.restosuite.cn" || u.Time == "" {
		t.Fatalf("%+v", u)
	}
	if wantsBECPU("PostgreSQL 连接打满怎么排查") {
		t.Fatal("连接打满不是 BE CPU")
	}
	if wantsBECPU("分析下beijing 环境 redis.bj.restosuite.cn 实例最近1小时cpu使用情况") {
		t.Fatal("beijing 不是 BE")
	}
}

func TestPrefetchStopsWithoutCatalog(t *testing.T) {
	s := &Service{}
	note := s.prefetchBECPU(context.Background(), "t1", "分析下 dorisdb.bj.restosuite.cn 最近1小时 be 节点的cpu")
	if !strings.Contains(note, "实例目录不可用") || strings.Contains(note, "Prometheus 查询失败") {
		t.Fatal(note)
	}
	if note := s.prefetchBECPU(context.Background(), "t1", "PostgreSQL 连接打满怎么排查"); note != "" {
		t.Fatal(note)
	}
}

func TestPickInstancePrefersExactHost(t *testing.T) {
	list := []store.Instance{
		{Name: "fuzzy", Host: "dorisdb.bj.restosuite.cn.extra", DBType: "starrocks"},
		{Name: "bj", Host: "10.1.2.3", DomainName: "dorisdb.bj.restosuite.cn", DBType: "starrocks", Port: 9030},
	}
	it := pickInstance(list, "dorisdb.bj.restosuite.cn")
	if it == nil || it.Name != "bj" {
		t.Fatalf("%+v", it)
	}
	if !isStarRocksLike(it.DBType) || isStarRocksLike("postgres") {
		t.Fatal(it.DBType)
	}
	fact := formatInstanceFact(it)
	if strings.Contains(fact, "password") || !strings.Contains(fact, "port=9030") {
		t.Fatal(fact)
	}
}
