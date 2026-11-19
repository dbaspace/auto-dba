package pgdiag

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/promclient"
)

const (
	poolBatchSize     = 20
	poolFallbackStep  = time.Hour
	poolRangeWindow   = 7 * 24 * time.Hour
	poolQueryBudget   = 45 * time.Second
	poolCacheTTL      = 3 * time.Minute
	maxPoolPodNames   = 200
	poolSubqueryRange = "7d:15m"
)

// poolPodNameRe 只接受 Kubernetes Pod 名，避免把请求里的字符串拼进别的 PromQL。
var poolPodNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

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

// ValidPoolPodNames 去重并校验 Pod 名。超过 200 个或含非法名字时返回错误。
func ValidPoolPodNames(raw []string) ([]string, error) {
	names := uniqueNames(raw)
	if len(names) > maxPoolPodNames {
		return nil, fmt.Errorf("Pod 名过多")
	}
	for _, name := range names {
		if len(name) > 253 || !poolPodNameRe.MatchString(name) {
			return nil, fmt.Errorf("Pod 名不合法")
		}
	}
	return names, nil
}

// MapPools 按 Pod 名查询最近 7 天的 HikariCP 指标。q 为空表示未配置 Prometheus。
// 先做瞬时聚合；聚合失败时退回步长 1 小时的区间查询。相同 Pod 组缓存 3 分钟。
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
	if ctx.Err() != nil {
		return map[string]PoolUsage{}, poolQueryNote(ctx, ctx.Err())
	}
	if cached, ok := poolCacheGet(names); ok {
		return cached, ""
	}
	out, err := mapPoolsInstant(ctx, q, names)
	if err != nil && ctx.Err() == nil {
		out, err = mapPoolsRange(ctx, q, names, poolFallbackStep)
	}
	if err == nil {
		poolCachePut(names, out)
		return out, ""
	}
	if out == nil {
		out = map[string]PoolUsage{}
	}
	return out, poolQueryNote(ctx, err)
}

func mapPoolsInstant(ctx context.Context, q PromQuerier, names []string) (map[string]PoolUsage, error) {
	batches := splitNameBatches(names, poolBatchSize, maxPodPromQL, buildPoolIdleExpr)
	out := map[string]PoolUsage{}
	for _, batch := range batches {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		part, err := queryPoolBatchInstant(ctx, q, batch)
		if err != nil {
			return nil, err
		}
		for pod, usage := range part {
			out[pod] = usage
		}
	}
	return out, nil
}

func queryPoolBatchInstant(ctx context.Context, q PromQuerier, pods []string) (map[string]PoolUsage, error) {
	type job struct {
		kind string
		expr string
	}
	jobs := []job{
		{kind: "latest", expr: buildPoolExpr(pods)},
		{kind: "active", expr: buildPoolPeakExpr("hikaricp_connections_active", pods)},
		{kind: "pending", expr: buildPoolPeakExpr("hikaricp_connections_pending", pods)},
		{kind: "limit", expr: buildPoolPeakExpr("hikaricp_connections_max", pods)},
		{kind: "total", expr: buildPoolSampleCountExpr(pods)},
		{kind: "idle", expr: buildPoolIdleExpr(pods)},
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var first error
	got := map[string][]promclient.Sample{}
	for _, item := range jobs {
		wg.Add(1)
		go func(item job) {
			defer wg.Done()
			samples, err := q.Query(ctx, item.expr, time.Now())
			mu.Lock()
			defer mu.Unlock()
			if err != nil && first == nil {
				first = err
			}
			got[item.kind] = samples
		}(item)
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	return usageFromInstant(got["latest"], got["active"], got["pending"], got["limit"], got["total"], got["idle"]), nil
}

func mapPoolsRange(ctx context.Context, q PromQuerier, names []string, step time.Duration) (map[string]PoolUsage, error) {
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
			samples, err := q.QueryRange(ctx, buildPoolExpr(batch), start, end, step)
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
	return out, first
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

func usageFromInstant(latest, activeMax, pendingMax, limitMax, total, idle []promclient.Sample) map[string]PoolUsage {
	current := map[string]map[string]float64{}
	for _, sm := range latest {
		name := strings.TrimSpace(sm.Metric["__name__"])
		pod := strings.TrimSpace(sm.Metric["pod"])
		if pod == "" || !poolMetricAllowed(name) {
			continue
		}
		value, ok := promclient.InstantValue(sm)
		if !ok {
			continue
		}
		metrics := current[pod]
		if metrics == nil {
			metrics = map[string]float64{}
			current[pod] = metrics
		}
		metrics[name] += value
	}
	peaksActive := oneValueByPod(activeMax)
	peaksPending := oneValueByPod(pendingMax)
	peaksLimit := oneValueByPod(limitMax)
	samples := oneValueByPod(total)
	idleSamples := oneValueByPod(idle)
	seen := map[string]struct{}{}
	for pod := range current {
		seen[pod] = struct{}{}
	}
	for _, set := range []map[string]float64{peaksActive, peaksPending, peaksLimit, samples, idleSamples} {
		for pod := range set {
			seen[pod] = struct{}{}
		}
	}
	out := make(map[string]PoolUsage, len(seen))
	for pod := range seen {
		metrics := current[pod]
		out[pod] = PoolUsage{
			HasData:      true,
			Current:      roundConn(metrics["hikaricp_connections"]),
			Active:       roundConn(metrics["hikaricp_connections_active"]),
			Idle:         roundConn(metrics["hikaricp_connections_idle"]),
			Pending:      roundConn(metrics["hikaricp_connections_pending"]),
			Max:          roundConn(metrics["hikaricp_connections_max"]),
			Min:          roundConn(metrics["hikaricp_connections_min"]),
			ActiveMax7d:  roundConn(peaksActive[pod]),
			PendingMax7d: roundConn(peaksPending[pod]),
			MaxLimit:     roundConn(peaksLimit[pod]),
			IdleSamples:  int(roundConn(idleSamples[pod])),
			Samples:      int(roundConn(samples[pod])),
		}
	}
	return out
}

func oneValueByPod(samples []promclient.Sample) map[string]float64 {
	out := map[string]float64{}
	for _, sm := range samples {
		pod := strings.TrimSpace(sm.Metric["pod"])
		value, ok := promclient.InstantValue(sm)
		if pod == "" || !ok {
			continue
		}
		if cur, exists := out[pod]; !exists || value > cur {
			out[pod] = value
		}
	}
	return out
}

func podMatcher(pods []string) string {
	parts := make([]string, len(pods))
	for i, pod := range pods {
		parts[i] = promRegex(pod)
	}
	return fmt.Sprintf(`pod=~"%s"`, strings.Join(parts, "|"))
}

func buildPoolPeakExpr(metric string, pods []string) string {
	return fmt.Sprintf(`max_over_time((sum by (pod) (%s{%s}))[%s])`, metric, podMatcher(pods), poolSubqueryRange)
}

func buildPoolSampleCountExpr(pods []string) string {
	return fmt.Sprintf(`count_over_time((sum by (pod) (hikaricp_connections{%s}))[%s])`, podMatcher(pods), poolSubqueryRange)
}

func buildPoolIdleExpr(pods []string) string {
	sel := podMatcher(pods)
	return fmt.Sprintf(`count_over_time((sum by (pod) (hikaricp_connections_active{%s}) == 0 and sum by (pod) (hikaricp_connections_idle{%s}) > 0 and sum by (pod) (hikaricp_connections{%s}) == sum by (pod) (hikaricp_connections_idle{%s}))[%s])`, sel, sel, sel, sel, poolSubqueryRange)
}

type poolCacheEntry struct {
	at    time.Time
	usage map[string]PoolUsage
}

var (
	poolCacheMu sync.Mutex
	poolCache   = map[string]poolCacheEntry{}
)

func poolCacheKey(names []string) string {
	cp := append([]string(nil), names...)
	sort.Strings(cp)
	return strings.Join(cp, "\n")
}

func poolCacheGet(names []string) (map[string]PoolUsage, bool) {
	poolCacheMu.Lock()
	defer poolCacheMu.Unlock()
	ent, ok := poolCache[poolCacheKey(names)]
	if !ok || time.Since(ent.at) > poolCacheTTL {
		delete(poolCache, poolCacheKey(names))
		return nil, false
	}
	return clonePoolUsage(ent.usage), true
}

func poolCachePut(names []string, usage map[string]PoolUsage) {
	if usage == nil {
		usage = map[string]PoolUsage{}
	}
	poolCacheMu.Lock()
	defer poolCacheMu.Unlock()
	poolCache[poolCacheKey(names)] = poolCacheEntry{at: time.Now(), usage: clonePoolUsage(usage)}
}

func clonePoolUsage(in map[string]PoolUsage) map[string]PoolUsage {
	out := make(map[string]PoolUsage, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
