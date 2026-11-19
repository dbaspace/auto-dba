package pgdiag

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"auto-dba/internal/promclient"
)

func resetPoolCache() {
	poolCacheMu.Lock()
	poolCache = map[string]poolCacheEntry{}
	poolCacheMu.Unlock()
}

func TestMapPoolsSumsPoolsAndIgnoresOtherMetrics(t *testing.T) {
	resetPoolCache()
	prom := &scriptProm{onQuery: func(q string) ([]promclient.Sample, error) {
		switch {
		case strings.Contains(q, "max_over_time") && strings.Contains(q, "hikaricp_connections_active"):
			return []promclient.Sample{instantSample("web-a", "", 6), instantSample("web-b", "", 1)}, nil
		case strings.Contains(q, "max_over_time") && strings.Contains(q, "hikaricp_connections_pending"):
			return []promclient.Sample{instantSample("web-a", "", 0)}, nil
		case strings.Contains(q, "max_over_time") && strings.Contains(q, "hikaricp_connections_max"):
			return []promclient.Sample{instantSample("web-a", "", 10)}, nil
		case strings.Contains(q, "count_over_time"):
			return nil, nil
		default:
			return []promclient.Sample{
				instantSample("web-a", "hikaricp_connections_active", 2),
				instantSample("web-a", "hikaricp_connections_active", 4),
				instantSample("web-a", "hikaricp_connections", 10),
				instantSample("web-a", "hikaricp_connections_timeout_total", 99),
				instantSample("web-b", "hikaricp_connections_active", 1),
			}, nil
		}
	}}
	got, note := MapPools(context.Background(), prom, []string{"web-a", "web-a"})
	if note != "" {
		t.Fatal(note)
	}
	usage := got["web-a"]
	if !usage.HasData || usage.Active != 6 || usage.ActiveMax7d != 6 || usage.Current != 10 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.PendingMax7d != 0 || got["web-b"].Active != 1 || got["web-b"].ActiveMax7d != 1 {
		t.Fatalf("pending=%d web-b=%+v", usage.PendingMax7d, got["web-b"])
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if len(prom.queries) != 6 || prom.rangeStep != 0 {
		t.Fatalf("queries=%d step=%s", len(prom.queries), prom.rangeStep)
	}
	joined := strings.Join(prom.queries, "\n")
	if strings.Contains(joined, "timeout_total") || strings.Contains(joined, "last_over_time") || !strings.Contains(joined, "max_over_time") || !strings.Contains(joined, "[7d:15m]") {
		t.Fatalf("query = %v", prom.queries)
	}
}

func TestMapPoolsFallsBackToHourlyRange(t *testing.T) {
	resetPoolCache()
	prom := &scriptProm{
		failHas:      "max_over_time",
		rangeSamples: []promclient.Sample{poolSeries("fallback-pod", "hikaricp_connections_active", 1, 3)},
	}
	got, note := MapPools(context.Background(), prom, []string{"fallback-pod"})
	if note != "" {
		t.Fatal(note)
	}
	usage := got["fallback-pod"]
	if !usage.HasData || usage.Active != 3 || usage.ActiveMax7d != 3 {
		t.Fatalf("usage = %+v", usage)
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if prom.rangeStep != time.Hour || prom.rangeSpan != poolRangeWindow {
		t.Fatalf("step=%s span=%s", prom.rangeStep, prom.rangeSpan)
	}
	for _, q := range prom.queries {
		if strings.Contains(q, "last_over_time") {
			t.Fatalf("query = %s", q)
		}
	}
}

func TestMapPoolsCachesRepeatQuery(t *testing.T) {
	resetPoolCache()
	prom := &scriptProm{}
	if _, note := MapPools(context.Background(), prom, []string{"cache-b", "cache-a"}); note != "" {
		t.Fatal(note)
	}
	prom.mu.Lock()
	first := len(prom.queries)
	prom.mu.Unlock()
	if _, note := MapPools(context.Background(), prom, []string{"cache-a", "cache-b"}); note != "" {
		t.Fatal(note)
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if first == 0 || len(prom.queries) != first {
		t.Fatalf("queries first=%d second=%d", first, len(prom.queries))
	}
}

func TestValidPoolPodNames(t *testing.T) {
	got, err := ValidPoolPodNames([]string{" web-a ", "web-a", "web-a"})
	if err != nil || len(got) != 1 || got[0] != "web-a" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if _, err := ValidPoolPodNames([]string{"bad name"}); err == nil || !strings.Contains(err.Error(), "不合法") {
		t.Fatal(err)
	}
	many := make([]string, maxPoolPodNames+1)
	for i := range many {
		many[i] = "pod-" + strconv.Itoa(i)
	}
	if _, err := ValidPoolPodNames(many); err == nil || !strings.Contains(err.Error(), "过多") {
		t.Fatal(err)
	}
}

func TestMapPoolsBatchesPodNames(t *testing.T) {
	resetPoolCache()
	prom := &scriptProm{}
	names := make([]string, 25)
	for i := range names {
		names[i] = "pod-" + strconv.Itoa(i)
	}
	_, note := MapPools(context.Background(), prom, names)
	if note != "" {
		t.Fatal(note)
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if len(prom.queries) != 12 || prom.rangeStep != 0 {
		t.Fatalf("queries = %d step=%s", len(prom.queries), prom.rangeStep)
	}
}

func TestMapPoolsFailureAndNil(t *testing.T) {
	resetPoolCache()
	if _, note := MapPools(context.Background(), nil, []string{"web"}); note != "未配置 Prometheus" {
		t.Fatal(note)
	}
	prom := &scriptProm{failHas: "hikaricp", rangeErr: errors.New("boom")}
	got, note := MapPools(context.Background(), prom, []string{"web"})
	if len(got) != 0 || !strings.Contains(note, "Prometheus 连接池查询失败，连接统计已返回") || !strings.Contains(note, "boom") {
		t.Fatalf("got=%v note=%s", got, note)
	}
}

func TestPoolAdviceRules(t *testing.T) {
	idle := PoolUsage{HasData: true, Current: 10, Active: 0, Idle: 10, Max: 10, Min: 10, MaxLimit: 10, IdleSamples: 10, Samples: 10}
	if !strings.Contains(PoolAdvice(idle), "一直空闲") || !strings.Contains(PoolAdvice(idle), "minimumIdle") {
		t.Fatal(PoolAdvice(idle))
	}
	full := PoolUsage{HasData: true, ActiveMax7d: 10, PendingMax7d: 2, Max: 10, MaxLimit: 10}
	if !strings.Contains(PoolAdvice(full), "打满") {
		t.Fatal(PoolAdvice(full))
	}
	wait := PoolUsage{HasData: true, ActiveMax7d: 3, PendingMax7d: 1, Max: 10, MaxLimit: 10}
	if !strings.Contains(PoolAdvice(wait), "没满") {
		t.Fatal(PoolAdvice(wait))
	}
	tight := PoolUsage{HasData: true, ActiveMax7d: 8, Max: 10, MaxLimit: 10}
	if !strings.Contains(PoolAdvice(tight), "余量不足") {
		t.Fatal(PoolAdvice(tight))
	}
	growing := PoolUsage{HasData: true, Current: 4, Active: 4, Idle: 0, Max: 10, MaxLimit: 10, ActiveMax7d: 4}
	if !strings.Contains(PoolAdvice(growing), "空闲连接已用完") {
		t.Fatal(PoolAdvice(growing))
	}
	normal := PoolUsage{HasData: true, Current: 6, Active: 2, Idle: 4, Max: 10, MaxLimit: 10, ActiveMax7d: 2}
	if !strings.Contains(PoolAdvice(normal), "活跃峰值 2") || !strings.Contains(PoolAdvice(normal), "未接近上限") {
		t.Fatal(PoolAdvice(normal))
	}
	if PoolAdvice(PoolUsage{}) != "" {
		t.Fatal("empty usage")
	}
}

func TestCombinePoolUsageSumsAndAdvises(t *testing.T) {
	byPod := map[string]PoolUsage{
		"web-a": {HasData: true, Current: 10, Idle: 10, Max: 10, MaxLimit: 10, ActiveMax7d: 0, IdleSamples: 10, Samples: 10},
		"web-b": {HasData: true, Current: 10, Idle: 10, Max: 10, MaxLimit: 10, ActiveMax7d: 0, IdleSamples: 10, Samples: 10},
	}
	got := CombinePoolUsage("web-a, web-b", byPod)
	if got.Current != 20 || got.Idle != 20 || !strings.Contains(got.Advice, "一直空闲") {
		t.Fatalf("combined = %+v", got)
	}
}

func instantSample(pod, name string, v float64) promclient.Sample {
	metric := map[string]string{"pod": pod}
	if name != "" {
		metric["__name__"] = name
	}
	return promclient.Sample{
		Metric: metric,
		Value:  []interface{}{float64(1), strconv.FormatFloat(v, 'f', -1, 64)},
	}
}

func poolSeries(pod, name string, vals ...float64) promclient.Sample {
	values := make([][]interface{}, len(vals))
	for i, v := range vals {
		values[i] = []interface{}{float64(i + 1), strconv.FormatFloat(v, 'f', -1, 64)}
	}
	return promclient.Sample{
		Metric: map[string]string{"pod": pod, "__name__": name},
		Values: values,
	}
}

func TestMapPoolsTimeout(t *testing.T) {
	resetPoolCache()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prom := &scriptProm{}
	_, note := MapPools(ctx, prom, []string{"web"})
	if note != "Prometheus 连接池查询超时，连接统计已返回" {
		t.Fatal(note)
	}
}
