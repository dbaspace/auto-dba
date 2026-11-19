package pgdiag

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

// ActivityTopologySQL 统计最近 5 分钟内有活动的客户端连接。语句固定，不接受外部拼接。
const ActivityTopologySQL = `SELECT client_addr::text, datname, count(*)::bigint
FROM pg_stat_activity
WHERE usename <> 'repl'
  AND (
    state_change >= now() - interval '5 minutes'
    OR query_start >= now() - interval '5 minutes'
  )
GROUP BY client_addr, datname
ORDER BY count(*) DESC`

const (
	podBatchSize        = 80
	maxPodPromQL        = 8000
	podQueryConcurrency = 4
	podQueryBudget      = 15 * time.Second
)

// ActivityRow 一条按客户端地址和数据库汇总的连接数。
type ActivityRow struct {
	Host        string
	Database    string
	Connections int64
}

// PodHit Prometheus 上一个地址对应的 Pod。多个 Pod 已去重并拼接。
type PodHit struct {
	Pod       string
	Namespace string
	Node      string
}

// PromQuerier 只使用瞬时查询。目标环境不支持 last_over_time。
type PromQuerier interface {
	Query(ctx context.Context, promql string, ts time.Time) ([]promclient.Sample, error)
}

// QueryActivity 连到实例的 postgres 库，执行固定的连接统计。
func QueryActivity(ctx context.Context, appSecret string, inst *store.Instance) ([]ActivityRow, error) {
	if inst == nil {
		return nil, fmt.Errorf("实例不存在")
	}
	if !IsPostgres(inst.DBType) {
		return nil, fmt.Errorf("仅支持 PostgreSQL 实例")
	}
	plain, err := decryptPassword(appSecret, inst)
	if err != nil {
		return nil, err
	}
	db, qctx, cancel, err := pingQuery(ctx, inst, plain)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer db.Close()
	return scanActivity(qctx, db)
}

func scanActivity(ctx context.Context, db *sql.DB) ([]ActivityRow, error) {
	rows, err := db.QueryContext(ctx, ActivityTopologySQL)
	if err != nil {
		return nil, fmt.Errorf("查询 pg_stat_activity: %w", err)
	}
	defer rows.Close()
	var out []ActivityRow
	for rows.Next() {
		var host, database sql.NullString
		var n int64
		if err := rows.Scan(&host, &database, &n); err != nil {
			return nil, err
		}
		out = append(out, ActivityRow{
			Host:        strings.TrimSpace(host.String),
			Database:    strings.TrimSpace(database.String),
			Connections: n,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []ActivityRow{}
	}
	return out, nil
}

// MapPods 按地址分批查询当前的 kube_pod_info。q 为空表示未配置 Prometheus。
// 先查 pod_ip，没有命中的地址再查 host_ip。任一查询失败时仍返回已经查到的结果。
func MapPods(ctx context.Context, q PromQuerier, addrs []string) (map[string]PodHit, string) {
	if q == nil {
		return map[string]PodHit{}, "未配置 Prometheus"
	}
	ips := uniquePodIPs(addrs)
	if len(ips) == 0 {
		return map[string]PodHit{}, ""
	}
	ctx, cancel := context.WithTimeout(ctx, podQueryBudget)
	defer cancel()
	hits, err1 := queryPodLabel(ctx, q, "pod_ip", ips)
	var missed []string
	for _, ip := range ips {
		if _, ok := hits[ip]; !ok {
			missed = append(missed, ip)
		}
	}
	var err2 error
	if len(missed) > 0 && ctx.Err() == nil {
		extra, err := queryPodLabel(ctx, q, "host_ip", missed)
		err2 = err
		for ip, hit := range extra {
			hits[ip] = hit
		}
	}
	if hits == nil {
		hits = map[string]PodHit{}
	}
	return hits, podQueryNote(ctx, err1, err2)
}

// LookupPodIP 返回可以拿去查 Prometheus 的地址。空地址、非法值和回环地址返回 false。
// PostgreSQL 的 client_addr::text 会带掩码，例如 10.100.129.2/32，这里先去掉再解析。
func LookupPodIP(host string) (string, bool) {
	ip := strings.TrimSpace(host)
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		ip = strings.TrimSpace(ip[:i])
	}
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsLoopback() || parsed.IsUnspecified() {
		return "", false
	}
	if v4 := parsed.To4(); v4 != nil {
		return v4.String(), true
	}
	return ip, true
}

func uniquePodIPs(addrs []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range addrs {
		ip, ok := LookupPodIP(raw)
		if !ok {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	return out
}

func queryPodLabel(ctx context.Context, q PromQuerier, label string, ips []string) (map[string]PodHit, error) {
	batches := splitPodBatches(label, ips, podBatchSize, maxPodPromQL)
	hits := map[string]PodHit{}
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
			samples, err := q.Query(ctx, buildPodExpr(label, batch), time.Now())
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			grouped := map[string][]promclient.Sample{}
			for _, sm := range samples {
				ip := strings.TrimSpace(sm.Metric[label])
				if ip == "" {
					continue
				}
				grouped[ip] = append(grouped[ip], sm)
			}
			for ip, list := range grouped {
				hit, ok := collapsePods(list)
				if ok {
					hits[ip] = hit
				}
			}
		}(batch)
	}
	wg.Wait()
	return hits, first
}

func splitPodBatches(label string, ips []string, size, maxLen int) [][]string {
	if size <= 0 {
		size = podBatchSize
	}
	if maxLen <= 0 {
		maxLen = maxPodPromQL
	}
	var out [][]string
	var cur []string
	for _, ip := range ips {
		if len(cur) == 0 {
			cur = []string{ip}
			continue
		}
		next := append(append([]string{}, cur...), ip)
		if len(next) > size || len(buildPodExpr(label, next)) > maxLen {
			out = append(out, cur)
			cur = []string{ip}
			continue
		}
		cur = next
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func buildPodExpr(label string, ips []string) string {
	parts := make([]string, len(ips))
	for i, ip := range ips {
		parts[i] = promRegex(ip)
	}
	return fmt.Sprintf(`kube_pod_info{%s=~"%s"}`, label, strings.Join(parts, "|"))
}

// promRegex 把 IP 写成 PromQL 字符串里的正则。点要写成 \\. ，Prometheus 还原后才是字面量点。
func promRegex(ip string) string {
	return strings.ReplaceAll(regexp.QuoteMeta(ip), `\`, `\\`)
}

func collapsePods(samples []promclient.Sample) (PodHit, bool) {
	var pods, nss, nodes []string
	seenPod := map[string]struct{}{}
	seenNS := map[string]struct{}{}
	seenNode := map[string]struct{}{}
	for _, sm := range samples {
		pod := cleanLabel(sm.Metric["pod"])
		if pod == "" {
			continue
		}
		if _, ok := seenPod[pod]; ok {
			continue
		}
		seenPod[pod] = struct{}{}
		pods = append(pods, pod)
		if ns := cleanLabel(sm.Metric["namespace"]); ns != "" {
			if _, ok := seenNS[ns]; !ok {
				seenNS[ns] = struct{}{}
				nss = append(nss, ns)
			}
		}
		if node := cleanLabel(sm.Metric["node"]); node != "" {
			if _, ok := seenNode[node]; !ok {
				seenNode[node] = struct{}{}
				nodes = append(nodes, node)
			}
		}
	}
	if len(pods) == 0 {
		return PodHit{}, false
	}
	return PodHit{
		Pod:       strings.Join(pods, ", "),
		Namespace: strings.Join(nss, ", "),
		Node:      strings.Join(nodes, ", "),
	}, true
}

func cleanLabel(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "<none>" {
		return ""
	}
	return v
}

func podQueryNote(ctx context.Context, errs ...error) string {
	var first error
	for _, err := range errs {
		if err != nil {
			first = err
			break
		}
	}
	if ctx.Err() != nil {
		return "Prometheus 查询超时，连接统计已返回"
	}
	if first == nil {
		return ""
	}
	reason := strings.TrimSpace(first.Error())
	if len([]rune(reason)) > 180 {
		reason = string([]rune(reason)[:180]) + "…"
	}
	if reason == "" {
		return "Prometheus 查询失败，连接统计已返回"
	}
	return "Prometheus 查询失败，连接统计已返回：" + reason
}
