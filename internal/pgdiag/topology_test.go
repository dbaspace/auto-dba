package pgdiag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"auto-dba/internal/promclient"
)

func TestActivityTopologySQLWindow(t *testing.T) {
	sqlText := ActivityTopologySQL
	for _, part := range []string{
		`datname AS "库"`,
		`client_addr::text AS "host"`,
		"usename <> 'repl'",
		"GROUP BY datname, client_addr",
		"ORDER BY count(*) DESC",
	} {
		if !strings.Contains(sqlText, part) {
			t.Fatalf("SQL 缺少 %s", part)
		}
	}
	if strings.Contains(sqlText, "5 minutes") || strings.Contains(sqlText, "state_change") || strings.Contains(sqlText, "query_start") {
		t.Fatalf("SQL 仍限制时间窗口: %s", sqlText)
	}
	if strings.Index(sqlText, "datname") > strings.Index(sqlText, "client_addr") {
		t.Fatalf("列顺序应为库、地址、连接数: %s", sqlText)
	}
}

func TestBuildPodExprEscapesDots(t *testing.T) {
	expr := buildPodExpr("pod_ip", []string{"10.1.2.3", "10.1.2.4"})
	if !strings.Contains(expr, `pod_ip=~"10\\.1\\.2\\.3|10\\.1\\.2\\.4"`) {
		t.Fatalf("expr = %s", expr)
	}
	if strings.Contains(expr, "last_over_time") || strings.Contains(expr, "[5m]") {
		t.Fatalf("expr = %s", expr)
	}
}

func TestSplitPodBatchesBySizeAndLength(t *testing.T) {
	ips := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		ips = append(ips, fmt.Sprintf("10.0.%d.%d", i/250, i%250))
	}
	got := splitPodBatches("pod_ip", ips, 80, maxPodPromQL)
	if len(got) != 2 || len(got[0]) != 80 || len(got[1]) != 20 {
		t.Fatalf("batches = %d / %d", len(got), len(got[0]))
	}
	long := make([]string, 10)
	for i := range long {
		long[i] = fmt.Sprintf("10.1.2.%d", i)
	}
	short := splitPodBatches("pod_ip", long, 80, 80)
	if len(short) < 2 {
		t.Fatalf("length split = %d", len(short))
	}
	for _, batch := range short {
		if len(buildPodExpr("pod_ip", batch)) > 80 && len(batch) > 1 {
			t.Fatalf("batch too long: %s", buildPodExpr("pod_ip", batch))
		}
	}
}

func TestLookupPodIPSkipsLocal(t *testing.T) {
	if _, ok := LookupPodIP(""); ok {
		t.Fatal("empty")
	}
	if _, ok := LookupPodIP("127.0.0.1"); ok {
		t.Fatal("loopback")
	}
	if _, ok := LookupPodIP("not-an-ip"); ok {
		t.Fatal("invalid")
	}
	ip, ok := LookupPodIP(" 10.1.2.3 ")
	if !ok || ip != "10.1.2.3" {
		t.Fatalf("got %q %v", ip, ok)
	}
	masked, ok := LookupPodIP("10.100.129.2/32")
	if !ok || masked != "10.100.129.2" {
		t.Fatalf("masked = %q %v", masked, ok)
	}
	if _, ok := LookupPodIP("127.0.0.1/8"); ok {
		t.Fatal("masked loopback")
	}
}

func TestMapPodsStripsCIDRMask(t *testing.T) {
	prom := &scriptProm{
		samples: map[string][]promclient.Sample{
			"10.100.129.2": {{
				Metric: map[string]string{
					"pod_ip":    "10.100.129.2",
					"pod":       "db-operation",
					"namespace": "default",
					"node":      "bj-test-1",
				},
			}},
		},
	}
	hits, note := MapPods(context.Background(), prom, []string{"10.100.129.2/32"})
	if note != "" {
		t.Fatal(note)
	}
	hit := hits["10.100.129.2"]
	if hit.Pod != "db-operation" || hit.Namespace != "default" || hit.Node != "bj-test-1" {
		t.Fatalf("hit = %+v", hit)
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if len(prom.queries) != 1 || !strings.Contains(prom.queries[0], `10\\.100\\.129\\.2`) || strings.Contains(prom.queries[0], "/32") {
		t.Fatalf("queries = %v", prom.queries)
	}
}

type scriptProm struct {
	mu           sync.Mutex
	queries      []string
	samples      map[string][]promclient.Sample
	failHas      string
	rangeSamples []promclient.Sample
	rangeErr     error
	rangeStep    time.Duration
	rangeSpan    time.Duration
}

func (s *scriptProm) QueryRange(_ context.Context, promql string, start, end time.Time, step time.Duration) ([]promclient.Sample, error) {
	s.mu.Lock()
	s.queries = append(s.queries, promql)
	s.rangeStep = step
	s.rangeSpan = end.Sub(start)
	samples := s.rangeSamples
	err := s.rangeErr
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return samples, nil
}

func (s *scriptProm) Query(_ context.Context, promql string, _ time.Time) ([]promclient.Sample, error) {
	s.mu.Lock()
	s.queries = append(s.queries, promql)
	s.mu.Unlock()
	if s.failHas != "" && strings.Contains(promql, s.failHas) {
		return nil, errors.New("boom")
	}
	var out []promclient.Sample
	for ip, samples := range s.samples {
		if strings.Contains(promql, regexpQuote(ip)) || strings.Contains(promql, ip) {
			out = append(out, samples...)
		}
	}
	return out, nil
}

func regexpQuote(ip string) string {
	return strings.ReplaceAll(ip, ".", `\\.`)
}

func TestMapPodsBatchesAndHostFallback(t *testing.T) {
	prom := &scriptProm{
		samples: map[string][]promclient.Sample{
			"10.0.0.1": {{
				Metric: map[string]string{
					"pod_ip":    "10.0.0.1",
					"pod":       "web-a",
					"namespace": "prod",
					"node":      "node-a",
				},
			}, {
				Metric: map[string]string{
					"pod_ip":    "10.0.0.1",
					"pod":       "web-a",
					"namespace": "prod",
					"node":      "node-a",
				},
			}},
			"10.0.0.2": {{
				Metric: map[string]string{
					"host_ip":   "10.0.0.2",
					"pod":       "hostnet",
					"namespace": "default",
					"node":      "node-b",
				},
			}},
		},
	}
	addrs := []string{"10.0.0.1", "10.0.0.1", "", "127.0.0.1", "10.0.0.2", "bad"}
	hits, note := MapPods(context.Background(), prom, addrs)
	if note != "" {
		t.Fatal(note)
	}
	if hits["10.0.0.1"].Pod != "web-a" || hits["10.0.0.1"].Namespace != "prod" {
		t.Fatalf("pod_ip hit = %+v", hits["10.0.0.1"])
	}
	if hits["10.0.0.2"].Pod != "hostnet" {
		t.Fatalf("host_ip hit = %+v", hits["10.0.0.2"])
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	var podQueries, hostQueries int
	for _, q := range prom.queries {
		switch {
		case strings.Contains(q, "pod_ip=~"):
			podQueries++
			if strings.Contains(q, "127") || strings.Contains(q, "bad") {
				t.Fatalf("queried skipped address: %s", q)
			}
		case strings.Contains(q, "host_ip=~"):
			hostQueries++
			if strings.Contains(q, "10.0.0.1") {
				t.Fatalf("host_ip queried a pod_ip hit: %s", q)
			}
		default:
			t.Fatalf("unexpected query %s", q)
		}
	}
	if podQueries != 1 || hostQueries != 1 {
		t.Fatalf("queries pod=%d host=%d %v", podQueries, hostQueries, prom.queries)
	}
}

func TestMapPodsManyAddressesUseFewQueries(t *testing.T) {
	prom := &scriptProm{samples: map[string][]promclient.Sample{}}
	addrs := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		addrs = append(addrs, fmt.Sprintf("10.8.%d.%d", i/250, i%250))
	}
	_, note := MapPods(context.Background(), prom, addrs)
	if note != "" {
		t.Fatal(note)
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if len(prom.queries) < 3 || len(prom.queries) > 8 {
		t.Fatalf("query count = %d, want a few batches", len(prom.queries))
	}
}

func TestMapPodsNilAndFailure(t *testing.T) {
	if _, note := MapPods(context.Background(), nil, []string{"10.0.0.1"}); note != "未配置 Prometheus" {
		t.Fatal(note)
	}
	prom := &scriptProm{failHas: "pod_ip=~"}
	hits, note := MapPods(context.Background(), prom, []string{"10.0.0.8"})
	if !strings.Contains(note, "Prometheus 查询失败，连接统计已返回") || !strings.Contains(note, "boom") {
		t.Fatal(note)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestMapPodsTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prom := &scriptProm{}
	_, note := MapPods(ctx, prom, []string{"10.0.0.9"})
	if note != "Prometheus 查询超时，连接统计已返回" {
		t.Fatal(note)
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if len(prom.queries) != 0 {
		t.Fatalf("queries = %v", prom.queries)
	}
}
