package pgdiag

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"auto-dba/internal/promclient"
)

func TestMapPoolsSumsPoolsAndIgnoresOtherMetrics(t *testing.T) {
	prom := &scriptProm{rangeSamples: []promclient.Sample{
		poolSeries("web-a", "hikaricp_connections_active", 1, 2),
		poolSeries("web-a", "hikaricp_connections_active", 3, 4),
		poolSeries("web-a", "hikaricp_connections", 10, 10),
		poolSeries("web-a", "hikaricp_connections_idle", 9, 8),
		poolSeries("web-a", "hikaricp_connections_pending", 0, 0),
		poolSeries("web-a", "hikaricp_connections_max", 10, 10),
		poolSeries("web-a", "hikaricp_connections_min", 10, 10),
		poolSeries("web-a", "hikaricp_connections_timeout_total", 99, 99),
		poolSeries("web-b", "hikaricp_connections_active", 1),
	}}
	got, note := MapPools(context.Background(), prom, []string{"web-a", "web-a"})
	if note != "" {
		t.Fatal(note)
	}
	usage := got["web-a"]
	if !usage.HasData || usage.Active != 6 || usage.ActiveMax7d != 6 || usage.Current != 10 {
		t.Fatalf("usage = %+v", usage)
	}
	if usage.PendingMax7d != 0 || got["web-b"].Active != 1 {
		t.Fatalf("pending=%d web-b=%+v", usage.PendingMax7d, got["web-b"])
	}
	prom.mu.Lock()
	defer prom.mu.Unlock()
	if len(prom.queries) != 1 || strings.Contains(prom.queries[0], "timeout_total") || strings.Contains(prom.queries[0], "last_over_time") {
		t.Fatalf("query = %v", prom.queries)
	}
	if !strings.Contains(prom.queries[0], "web-a") || prom.rangeStep != poolRangeStep || prom.rangeSpan != poolRangeWindow {
		t.Fatalf("step=%s span=%s query=%s", prom.rangeStep, prom.rangeSpan, prom.queries[0])
	}
}

func TestMapPoolsBatchesPodNames(t *testing.T) {
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
	if len(prom.queries) != 2 {
		t.Fatalf("queries = %d", len(prom.queries))
	}
}

func TestMapPoolsFailureAndNil(t *testing.T) {
	if _, note := MapPools(context.Background(), nil, []string{"web"}); note != "未配置 Prometheus" {
		t.Fatal(note)
	}
	prom := &scriptProm{rangeErr: errors.New("boom")}
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prom := &scriptProm{}
	_, note := MapPools(ctx, prom, []string{"web"})
	if note != "Prometheus 连接池查询超时，连接统计已返回" {
		t.Fatal(note)
	}
}
