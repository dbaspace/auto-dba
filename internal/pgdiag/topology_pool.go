package pgdiag

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/promclient"
)

const (
	poolBatchSize   = 20
	poolRangeStep   = 15 * time.Minute
	poolRangeWindow = 7 * 24 * time.Hour
	poolQueryBudget = 45 * time.Second
)

const poolMetricNames = "hikaricp_connections|hikaricp_connections_active|hikaricp_connections_idle|hikaricp_connections_pending|hikaricp_connections_max|hikaricp_connections_min"

// PoolUsage 一个 Pod 最近 7 天的连接池汇总。数值是连接条数。
type PoolUsage struct {
	HasData      bool
	Current      int64
	Active       int64
	Idle         int64
	Pending      int64
	Max          int64
	Min          int64
	ActiveMax7d  int64
	PendingMax7d int64
	MaxLimit     int64
	IdleSamples  int
	Samples      int
	Advice       string
}

// MapPools 按 Pod 名查询最近 7 天的 HikariCP 指标。q 为空表示未配置 Prometheus。
// 查询失败时仍返回已经汇总的结果。
func MapPools(ctx context.Context, q PromQuerier, pods []string) (map[string]PoolUsage, string) {
	names := uniqueNames(pods)
	if q == nil {
		return map[string]PoolUsage{}, "未配置 Prometheus"
	}
	if len(names) == 0 {
		return map[string]PoolUsage{}, ""
	}
	ctx, cancel := context.WithTimeout(ctx, poolQueryBudget)
	defer cancel()
	end := time.Now()
	start := end.Add(-poolRangeWindow)
	batches := splitNameBatches(names, poolBatchSize, maxPodPromQL, buildPoolExpr)
	out := map[string]PoolUsage{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, podQueryConcurrency)
	var first error
	for _, batch := range batches {
		if ctx.Err() != nil {
			mu.Lock()
			if first == nil {
				first = ctx.Err()
			}
			mu.Unlock()
			break
		}
		wg.Add(1)
		go func(batch []string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				if first == nil {
					first = ctx.Err()
				}
				mu.Unlock()
				return
			}
			defer func() { <-sem }()
			samples, err := q.QueryRange(ctx, buildPoolExpr(batch), start, end, poolRangeStep)
			folded := foldPoolSamples(samples)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && first == nil {
				first = err
			}
			for pod, usage := range folded {
				out[pod] = usage
			}
		}(batch)
	}
	wg.Wait()
	if out == nil {
		out = map[string]PoolUsage{}
	}
	return out, poolQueryNote(ctx, first)
}

// CombinePoolUsage 把一行里逗号拼接的多个 Pod 用量相加，并按合计生成分析。
func CombinePoolUsage(joined string, byPod map[string]PoolUsage) PoolUsage {
	var sum PoolUsage
	for _, name := range splitJoinedPods(joined) {
		usage, ok := byPod[name]
		if !ok || !usage.HasData {
			continue
		}
		sum.HasData = true
		sum.Current += usage.Current
		sum.Active += usage.Active
		sum.Idle += usage.Idle
		sum.Pending += usage.Pending
		sum.Max += usage.Max
		sum.Min += usage.Min
		sum.ActiveMax7d += usage.ActiveMax7d
		sum.PendingMax7d += usage.PendingMax7d
		sum.MaxLimit += usage.MaxLimit
		sum.IdleSamples += usage.IdleSamples
		sum.Samples += usage.Samples
	}
	if sum.HasData {
		sum.Advice = PoolAdvice(sum)
	}
	return sum
}

// PodNamesFromHits 收集地址映射里出现过的 Pod 名。
func PodNamesFromHits(hits map[string]PodHit) []string {
	var raw []string
	for _, hit := range hits {
		raw = append(raw, splitJoinedPods(hit.Pod)...)
	}
	return uniqueNames(raw)
}

// JoinPromNotes 合并两段 Prometheus 说明。相同的只保留一次。
func JoinPromNotes(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" || a == b {
		return a
	}
	return a + "；" + b
}

// PoolAdvice 按最近 7 天的汇总写一句建议。没有数据时返回空字符串。
func PoolAdvice(u PoolUsage) string {
	if !u.HasData {
		return ""
	}
	limit := u.MaxLimit
	if limit <= 0 {
		limit = u.Max
	}
	switch {
	case u.PendingMax7d > 0 && limit > 0 && u.ActiveMax7d >= limit:
		return "这 7 天里池曾打满，业务线程等过连接。先查慢 SQL、未归还连接和长事务；确认数据库还能加连接后，再提高 maximumPoolSize。"
	case u.PendingMax7d > 0:
		return "这 7 天有线程等过连接，池却没满。检查建连是否变慢、获取超时，以及数据库是否拒绝新连接。"
	case limit > 0 && u.ActiveMax7d*5 >= limit*4:
		return "这 7 天余量不足。突发流量时容易排队，先看慢 SQL 和事务时长。"
	case u.Samples > 0 && u.IdleSamples*10 >= u.Samples*9:
		return "最近 7 天连接基本一直空闲，没有请求占用连接。这些空闲连接仍占用 PostgreSQL 连接名额。若该 Pod 应该在处理流量，先确认请求有没有打到它；若业务就是低峰，并且最小空闲等于最大连接，空闲连接不会回收，可把 minimumIdle 调低。"
	case u.Active > 0 && u.Idle == 0 && u.Current < u.Max:
		return "此刻空闲连接已用完，池还会继续建连。关注是否很快顶到最大。"
	default:
		return fmt.Sprintf("最近 7 天活跃峰值 %d、等待峰值 %d，未接近上限。", u.ActiveMax7d, u.PendingMax7d)
	}
}

func buildPoolExpr(pods []string) string {
	parts := make([]string, len(pods))
	for i, pod := range pods {
		parts[i] = promRegex(pod)
	}
	return fmt.Sprintf(`{__name__=~"%s",pod=~"%s"}`, poolMetricNames, strings.Join(parts, "|"))
}

func foldPoolSamples(samples []promclient.Sample) map[string]PoolUsage {
	// pod -> metric -> timestamp -> summed value
	raw := map[string]map[string]map[int64]float64{}
	for _, sm := range samples {
		name := strings.TrimSpace(sm.Metric["__name__"])
		pod := strings.TrimSpace(sm.Metric["pod"])
		if pod == "" || !poolMetricAllowed(name) {
			continue
		}
		metrics := raw[pod]
		if metrics == nil {
			metrics = map[string]map[int64]float64{}
			raw[pod] = metrics
		}
		points := metrics[name]
		if points == nil {
			points = map[int64]float64{}
			metrics[name] = points
		}
		for _, pt := range promclient.SampleToPoints(sm) {
			points[pt.T] += pt.V
		}
	}
	out := make(map[string]PoolUsage, len(raw))
	for pod, metrics := range raw {
		usage := summarizePool(metrics)
		if usage.HasData {
			out[pod] = usage
		}
	}
	return out
}

func summarizePool(metrics map[string]map[int64]float64) PoolUsage {
	current, _, currentN := lastAndMax(metrics["hikaricp_connections"])
	active, activeMax, activeN := lastAndMax(metrics["hikaricp_connections_active"])
	idle, _, idleN := lastAndMax(metrics["hikaricp_connections_idle"])
	pending, pendingMax, pendingN := lastAndMax(metrics["hikaricp_connections_pending"])
	maxLast, maxPeak, maxN := lastAndMax(metrics["hikaricp_connections_max"])
	minLast, _, minN := lastAndMax(metrics["hikaricp_connections_min"])
	if currentN+activeN+idleN+pendingN+maxN+minN == 0 {
		return PoolUsage{}
	}
	idleSamples, samples := countIdleSamples(
		metrics["hikaricp_connections_active"],
		metrics["hikaricp_connections_idle"],
		metrics["hikaricp_connections"],
	)
	return PoolUsage{
		HasData:      true,
		Current:      roundConn(current),
		Active:       roundConn(active),
		Idle:         roundConn(idle),
		Pending:      roundConn(pending),
		Max:          roundConn(maxLast),
		Min:          roundConn(minLast),
		ActiveMax7d:  roundConn(activeMax),
		PendingMax7d: roundConn(pendingMax),
		MaxLimit:     roundConn(maxPeak),
		IdleSamples:  idleSamples,
		Samples:      samples,
	}
}

func countIdleSamples(active, idle, current map[int64]float64) (idleN, total int) {
	for ts, cur := range current {
		a, aok := active[ts]
		i, iok := idle[ts]
		if !aok || !iok {
			continue
		}
		total++
		if roundConn(a) == 0 && roundConn(i) > 0 && roundConn(cur) == roundConn(i) {
			idleN++
		}
	}
	return idleN, total
}

func lastAndMax(points map[int64]float64) (last, max float64, n int) {
	if len(points) == 0 {
		return 0, 0, 0
	}
	var latest int64
	first := true
	for ts, value := range points {
		if first || value > max {
			max = value
		}
		if first || ts >= latest {
			latest = ts
			last = value
		}
		first = false
	}
	return last, max, len(points)
}

func roundConn(v float64) int64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return int64(math.Round(v))
}

func poolMetricAllowed(name string) bool {
	switch name {
	case "hikaricp_connections",
		"hikaricp_connections_active",
		"hikaricp_connections_idle",
		"hikaricp_connections_pending",
		"hikaricp_connections_max",
		"hikaricp_connections_min":
		return true
	default:
		return false
	}
}

func splitJoinedPods(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func uniqueNames(names []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func splitNameBatches(names []string, size, maxLen int, expr func([]string) string) [][]string {
	if size <= 0 {
		size = poolBatchSize
	}
	if maxLen <= 0 {
		maxLen = maxPodPromQL
	}
	var out [][]string
	var cur []string
	for _, name := range names {
		if len(cur) == 0 {
			cur = []string{name}
			continue
		}
		next := append(append([]string{}, cur...), name)
		if len(next) > size || len(expr(next)) > maxLen {
			out = append(out, cur)
			cur = []string{name}
			continue
		}
		cur = next
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func poolQueryNote(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "Prometheus 连接池查询超时，连接统计已返回"
	}
	if err == nil {
		return ""
	}
	reason := strings.TrimSpace(err.Error())
	if len([]rune(reason)) > 180 {
		reason = string([]rune(reason)[:180]) + "…"
	}
	if reason == "" {
		return "Prometheus 连接池查询失败，连接统计已返回"
	}
	return "Prometheus 连接池查询失败，连接统计已返回：" + reason
}
