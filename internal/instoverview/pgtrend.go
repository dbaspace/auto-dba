package instoverview

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const pgTrendStepShort = 5 * time.Minute
const pgTrendStepLong = 15 * time.Minute

// PGQueryStep 短区间保持 5 分钟。满 3 天改为 15 分钟，避免 7 天曲线把网关打满。rate 窗口仍是 5 分钟。
func PGQueryStep(start, end time.Time) time.Duration {
	if end.Sub(start) >= 3*24*time.Hour {
		return pgTrendStepLong
	}
	return pgTrendStepShort
}

// PGInstanceSelector 只匹配这些主机或域名。instance 允许带 exporter 端口。没有地址时返回空串，调用方不发查询。
func PGInstanceSelector(addrs []string) string {
	alt := pgPromAlt(addrs)
	if alt == "" {
		return ""
	}
	return `instance=~"(` + alt + `)(:[0-9]+)?"`
}

// PGDomainSelector 匹配挂在 domain 标签上的 exporter。地址规则与 instance 相同。
func PGDomainSelector(addrs []string) string {
	alt := pgPromAlt(addrs)
	if alt == "" {
		return ""
	}
	return `domain=~"(` + alt + `)(:[0-9]+)?"`
}

// PGEither 把同一条指标的 instance 查询和 domain 查询并起来。左边有数时 or 不会再用右边。
func PGEither(instSel, domSel string, build func(string) string) string {
	if build == nil {
		return ""
	}
	left := build(instSel)
	right := build(domSel)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + " or " + right
	}
}

// PGNameSelector 收窄云监控的 d_b_instance_name。
func PGNameSelector(names []string) string {
	alt := pgPromAlt(names)
	if alt == "" {
		return ""
	}
	return `d_b_instance_name=~"` + alt + `"`
}

func pgPromAlt(vals []string) string {
	seen := map[string]struct{}{}
	clean := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		clean = append(clean, v)
	}
	sort.Strings(clean)
	parts := make([]string, 0, len(clean))
	for _, v := range clean {
		parts = append(parts, strings.ReplaceAll(regexp.QuoteMeta(v), `\`, `\\`))
	}
	return strings.Join(parts, "|")
}

// PGSizeExpr 库文件合计。selector 为空时不查询。
func PGSizeExpr(instanceSel string) string {
	if instanceSel == "" {
		return ""
	}
	return `sum by (instance, domain) (pg_database_size_bytes{` + instanceSel + `})`
}

// PGHostnameExactSelector 精确匹配 Grafana 变量 hostname="$db"。不加端口通配，避免把别的目标算进来。
func PGHostnameExactSelector(vals []string) string {
	alt := pgPromAlt(vals)
	if alt == "" {
		return ""
	}
	return `hostname=~"` + alt + `"`
}

// PGTPSByHostname 是单库 Grafana TPS：该 hostname 上提交速率与回滚速率相加。多台时按 hostname 分开，不把 instance、domain 再 or 进来。
func PGTPSByHostname(vals []string) string {
	sel := PGHostnameExactSelector(vals)
	if sel == "" {
		return ""
	}
	return `sum by (hostname) (rate(pg_stat_database_xact_commit{` + sel + `}[5m])) + sum by (hostname) (rate(pg_stat_database_xact_rollback{` + sel + `}[5m]))`
}

// PGQPSByHostname 是单库 Grafana QPS：该 hostname 上的语句调用速率。
func PGQPSByHostname(vals []string) string {
	sel := PGHostnameExactSelector(vals)
	if sel == "" {
		return ""
	}
	return `sum by (hostname) (rate(pg_stat_statements_calls_total{` + sel + `}[5m]))`
}

// PGOverTime 在分析窗口上按 1 分钟取最大或平均。7 天以内用 1 分钟，更长用 5 分钟，避免把面板上的尖峰采样掉。
func PGOverTime(inner, agg string, start, end time.Time) string {
	inner = strings.TrimSpace(inner)
	agg = strings.TrimSpace(agg)
	if inner == "" || agg == "" {
		return ""
	}
	return agg + "((" + inner + ")[" + pgLookback(start, end) + "])"
}

func pgLookback(start, end time.Time) string {
	d := end.Sub(start)
	if d < time.Minute {
		d = time.Minute
	}
	step := time.Minute
	if d > 7*24*time.Hour {
		step = 5 * time.Minute
	}
	return strconv.FormatInt(int64(d/time.Second), 10) + "s:" + strconv.FormatInt(int64(step/time.Second), 10) + "s"
}

// PGHostnameCandidates 是一台实例可能出现在 Grafana $db 里的值。实例名优先，其次域名，最后地址。
func PGHostnameCandidates(name, domain, host string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, v := range []string{strings.TrimSpace(name), strings.TrimSpace(domain), strings.TrimSpace(host)} {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// PGHostCandidate 是一台实例的 hostname 候选。
type PGHostCandidate struct {
	ID    string
	Names []string
}

// PickPGHostnames 每台只保留第一个确实有序列的 hostname。不同候选同时有数时不取较大的那条。
func PickPGHostnames(rows []PGHostCandidate, present map[string]bool) map[string]string {
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		for _, name := range row.Names {
			if present[name] {
				out[row.ID] = name
				break
			}
		}
	}
	return out
}

// PGLocksExpr 锁数量。
func PGLocksExpr(instanceSel string) string {
	if instanceSel == "" {
		return ""
	}
	return `sum by (instance, domain) (pg_locks_count{` + instanceSel + `})`
}

// PGDeadlocksExpr 死锁速率。
func PGDeadlocksExpr(instanceSel string) string {
	if instanceSel == "" {
		return ""
	}
	return `sum by (instance, domain) (rate(pg_stat_database_deadlocks{` + instanceSel + `}[5m]))`
}

// PGCloudFileExpr 腾讯云数据盘或日志盘，单位是 GB。
func PGCloudFileExpr(nameSel, metric string) string {
	if nameSel == "" || strings.TrimSpace(metric) == "" {
		return ""
	}
	return `max by (d_b_instance_name, idc) (` + metric + `{` + nameSel + `})`
}

// PGSlotActiveExpr 激活中的复制槽数。
func PGSlotActiveExpr(sel string) string {
	if sel == "" {
		return ""
	}
	return `sum by (instance, domain) (pg_replication_slots_active{` + sel + `})`
}

// PGSlotTotalExpr 复制槽个数。
func PGSlotTotalExpr(sel string) string {
	if sel == "" {
		return ""
	}
	return `count by (instance, domain) (pg_replication_slots_active{` + sel + `})`
}

// PGSlotMaxExpr 复制槽上限。
func PGSlotMaxExpr(sel string) string {
	if sel == "" {
		return ""
	}
	return `max by (instance, domain) (pg_settings_max_replication_slots{` + sel + `})`
}

// PGSlotWALExpr 槽保留的 WAL 字节。没有这条指标时再试复制延迟。
func PGSlotWALExpr(sel string) string {
	if sel == "" {
		return ""
	}
	return `max by (instance, domain) (pg_replication_slots_pg_wal_lsn_diff{` + sel + `})`
}

// PGSlotWALAltExpr 是槽 WAL 的备用指标。
func PGSlotWALAltExpr(sel string) string {
	if sel == "" {
		return ""
	}
	return `max by (instance, domain) (pg_stat_replication_pg_wal_lsn_diff{` + sel + `})`
}

// PGStorageRateExpr 是腾讯云当前磁盘使用率，数值已经是 0–100，与 Grafana 图例一致。
func PGStorageRateExpr(nameSel string) string {
	if nameSel == "" {
		return ""
	}
	return `max by (idc, d_b_instance_name) (qce_postgres_storagerate_max{` + nameSel + `})`
}

// PGCPUExpr 云监控 CPU，已换算成 0–1。
func PGCPUExpr(nameSel string) string {
	if nameSel == "" {
		return ""
	}
	return `max by (d_b_instance_name, idc) (qce_postgres_cpu_max{` + nameSel + `}) / 100`
}

// PGPoint 是区间曲线上的一个点。T 为 Unix 秒。
type PGPoint struct {
	T int64
	V float64
}

// PGInstanceInput 是一台 PostgreSQL 在分析窗口内的样本。容量为 0 表示没有购买磁盘。
type PGInstanceInput struct {
	ID               string
	Name             string
	App              string
	Domain           string
	Host             string
	EnvName          string
	Days             int
	Start            time.Time
	End              time.Time
	Capacity         int64
	CurrentRate      *float64
	SizeEnd          *float64
	SizeStart        *float64
	SizeEndTimeout   bool
	SizeStartTimeout bool
	QPS              []PGPoint
	TPS              []PGPoint
	Locks            []PGPoint
	Deadlocks        []PGPoint
	CPU              []PGPoint
	QPSTimeout       bool
	TPSTimeout       bool
	LocksTimeout     bool
	DeadlocksTimeout bool
	CPUTimeout       bool
	SlotActive       *float64
	SlotTotal        *float64
	SlotMax          *float64
	SlotTimeout      bool
	SlotWAL          []PGPoint
	SlotWALTimeout   bool
	QPSStat          *PGRateStat
	TPSStat          *PGRateStat
	CPUStat          *PGRateStat
}

// PGRateStat 是按 Grafana 同一条表达式算出的平均、峰值和前后半段。
type PGRateStat struct {
	Known   bool
	Avg     float64
	Max     float64
	First   float64
	Last    float64
	HasHalf bool
}

// PGStorage 是库文件相对购买磁盘的增长。库文件不含 WAL。
type PGStorage struct {
	DiskTrend
	CapacityKnown bool `json:"capacity_known"`
	RateCurrent   bool `json:"rate_current,omitempty"`
}

// PGMetric 是一项区间指标的平均、峰值和前后半段对照。
type PGMetric struct {
	Known    bool    `json:"known"`
	Reason   string  `json:"reason,omitempty"`
	Avg      float64 `json:"avg"`
	Max      float64 `json:"max"`
	FirstAvg float64 `json:"first_avg,omitempty"`
	LastAvg  float64 `json:"last_avg,omitempty"`
	HasHalf  bool    `json:"has_half"`
	Level    string  `json:"level"`
	Advice   string  `json:"advice"`
}

// PGTrendItem 是报告里的一台实例。
type PGTrendItem struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	App       string      `json:"app,omitempty"`
	Domain    string      `json:"domain,omitempty"`
	Host      string      `json:"host"`
	EnvName   string      `json:"env_name"`
	Storage   *PGStorage  `json:"storage,omitempty"`
	QPS       *PGMetric   `json:"qps,omitempty"`
	TPS       *PGMetric   `json:"tps,omitempty"`
	Locks     *PGMetric   `json:"locks,omitempty"`
	Deadlocks *PGMetric   `json:"deadlocks,omitempty"`
	CPU       *PGMetric   `json:"cpu,omitempty"`
	Slots     *PGSlotStat `json:"slots,omitempty"`
	SlotWAL   *PGMetric   `json:"slot_wal,omitempty"`
	Level     string      `json:"level"`
	Advice    string      `json:"advice"`
}

// PGSlotStat 是一台实例的复制槽。Active 为激活数，Total 为槽数，Max 为上限。
type PGSlotStat struct {
	Known  bool     `json:"known"`
	Active float64  `json:"active"`
	Total  float64  `json:"total"`
	Max    *float64 `json:"max,omitempty"`
	Level  string   `json:"level"`
	Advice string   `json:"advice"`
	Reason string   `json:"reason,omitempty"`
}

// PGTrendSummary 是报告顶部的汇总结论。
type PGTrendSummary struct {
	Instances     int      `json:"instances"`
	Hosts         int      `json:"hosts"`
	Danger        int      `json:"danger"`
	FullRisk      int      `json:"full_risk"`
	Deadlocks     int      `json:"deadlocks"`
	CPUHigh       int      `json:"cpu_high"`
	Throughput    int      `json:"throughput"`
	NoSample      int      `json:"no_sample"`
	InactiveSlots int      `json:"inactive_slots"`
	Conclusions   []string `json:"conclusions"`
}

// AnalyzePGInstance 汇总一台实例的库增长、QPS/TPS、锁和 CPU。
func AnalyzePGInstance(in PGInstanceInput) PGTrendItem {
	days := in.Days
	if days < 1 {
		days = 1
	}
	item := PGTrendItem{
		ID: in.ID, Name: in.Name, App: in.App, Domain: in.Domain, Host: in.Host, EnvName: in.EnvName,
		Storage:   pgStorage(days, in.Capacity, in.CurrentRate, in.SizeEnd, in.SizeStart, in.SizeEndTimeout, in.SizeStartTimeout),
		QPS:       pgMetricPrefer(in.QPSStat, in.QPS, in.Start, in.End, "qps", in.QPSTimeout),
		TPS:       pgMetricPrefer(in.TPSStat, in.TPS, in.Start, in.End, "tps", in.TPSTimeout),
		Locks:     pgMetric(in.Locks, in.Start, in.End, "locks", in.LocksTimeout),
		Deadlocks: pgMetric(in.Deadlocks, in.Start, in.End, "deadlocks", in.DeadlocksTimeout),
		CPU:       pgMetricPrefer(in.CPUStat, in.CPU, in.Start, in.End, "cpu", in.CPUTimeout),
		Slots:     pgSlots(in.SlotActive, in.SlotTotal, in.SlotMax, in.SlotTimeout),
		SlotWAL:   pgMetric(in.SlotWAL, in.Start, in.End, "slot_wal", in.SlotWALTimeout),
	}
	item.Level, item.Advice = pickPGAdvice(item.Storage, item.Deadlocks, item.CPU, item.TPS, item.Slots)
	return item
}

func pgStorage(days int, capacity int64, current, end, start *float64, endTimeout, startTimeout bool) *PGStorage {
	s := &PGStorage{}
	applyCurrent := func() {
		if current == nil || *current < 0 || math.IsNaN(*current) || math.IsInf(*current, 0) {
			return
		}
		s.Rate = *current
		s.RateCurrent = true
	}
	if end == nil || *end < 0 || math.IsNaN(*end) {
		applyCurrent()
		if s.RateCurrent {
			s.Level = "ok"
			s.Advice = "当前磁盘使用率"
			return s
		}
		s.Level = "muted"
		if endTimeout {
			s.Advice = "库大小查询超时"
		} else {
			s.Advice = "没有库大小样本"
		}
		return s
	}
	s.UsedBytes = *end
	if capacity > 0 {
		s.CapacityKnown = true
		s.SizeBytes = float64(capacity)
		s.Rate = *end / float64(capacity) * 100
	}
	applyCurrent()
	if start == nil || *start < 0 || math.IsNaN(*start) {
		if startTimeout {
			s.Level = "muted"
			s.Advice = "库大小查询超时"
			return s
		}
		if s.CapacityKnown {
			s.Level, s.Advice = trendAdvice(&s.DiskTrend, days)
			return s
		}
		s.Level = "muted"
		s.Advice = fmt.Sprintf("没有 %d 天前的样本，也没有购买容量", days)
		return s
	}
	s.HasPrev = true
	s.PrevUsedBytes = *start
	s.GrowthBytes = *end - *start
	s.DailyBytes = s.GrowthBytes / float64(days)
	if s.CapacityKnown {
		s.Level, s.Advice = trendAdvice(&s.DiskTrend, days)
		return s
	}
	s.Level = "ok"
	s.Advice = "没有购买容量，不能估算写满天数"
	return s
}

func pgMetricPrefer(stat *PGRateStat, points []PGPoint, start, end time.Time, kind string, timedOut bool) *PGMetric {
	if stat != nil {
		return pgMetricFromRate(stat, kind, timedOut)
	}
	return pgMetric(points, start, end, kind, timedOut)
}

func pgMetricFromRate(s *PGRateStat, kind string, timedOut bool) *PGMetric {
	if s == nil || !s.Known {
		return pgMetric(nil, time.Time{}, time.Time{}, kind, timedOut)
	}
	m := &PGMetric{Known: true, Avg: s.Avg, Max: s.Max, FirstAvg: s.First, LastAvg: s.Last, HasHalf: s.HasHalf}
	return advisePGMetric(m, kind)
}

func pgMetric(points []PGPoint, start, end time.Time, kind string, timedOut bool) *PGMetric {
	avg, max, first, last, hasHalf, ok := summarizePGPoints(points, start, end)
	m := &PGMetric{Known: ok, Avg: avg, Max: max, FirstAvg: first, LastAvg: last, HasHalf: hasHalf}
	if !ok {
		m.Level = "muted"
		if timedOut {
			m.Reason = "查询超时"
			m.Advice = "查询超时"
		} else if kind == "qps" {
			m.Reason = "没有语句统计"
			m.Advice = "没有语句统计"
		} else {
			m.Reason = "没有样本"
			m.Advice = "没有样本"
		}
		return m
	}
	return advisePGMetric(m, kind)
}

func advisePGMetric(m *PGMetric, kind string) *PGMetric {
	switch kind {
	case "deadlocks":
		adviseDeadlocks(m)
	case "cpu":
		adviseCPU(m)
	case "tps":
		adviseTPS(m)
	default:
		m.Level = "ok"
		m.Advice = "仅展示峰值"
	}
	return m
}

func summarizePGPoints(points []PGPoint, start, end time.Time) (avg, max, first, last float64, hasHalf, ok bool) {
	var n, nFirst, nLast int
	var sum, sumFirst, sumLast float64
	max = math.Inf(-1)
	var mid int64
	if !end.IsZero() {
		mid = start.Add(end.Sub(start) / 2).Unix()
	}
	for _, p := range points {
		if math.IsNaN(p.V) || math.IsInf(p.V, 0) || p.V < 0 {
			continue
		}
		ok = true
		n++
		sum += p.V
		if p.V > max {
			max = p.V
		}
		if end.IsZero() {
			continue
		}
		if p.T < mid {
			nFirst++
			sumFirst += p.V
		} else {
			nLast++
			sumLast += p.V
		}
	}
	if !ok {
		return 0, 0, 0, 0, false, false
	}
	avg = sum / float64(n)
	if nFirst > 0 && nLast > 0 {
		hasHalf = true
		first = sumFirst / float64(nFirst)
		last = sumLast / float64(nLast)
	}
	return avg, max, first, last, hasHalf, true
}

func adviseDeadlocks(m *PGMetric) {
	switch {
	case m.Max >= 0.1:
		m.Level = "danger"
		m.Advice = "有死锁，查日志并统一加锁顺序"
	case m.Max >= 0.01:
		m.Level = "warn"
		m.Advice = "有死锁，查日志并统一加锁顺序"
	default:
		m.Level = "ok"
		m.Advice = "没有明显死锁"
	}
	noteHalfRise(m)
}

func adviseCPU(m *PGMetric) {
	switch {
	case m.Max >= 1:
		m.Level = "danger"
		m.Advice = "CPU 峰值达到 100%"
	case m.Max >= 0.85:
		m.Level = "warn"
		m.Advice = "CPU 峰值达到 85%"
	default:
		m.Level = "ok"
		m.Advice = "CPU 正常"
	}
	noteHalfRise(m)
}

func adviseTPS(m *PGMetric) {
	m.Level = "ok"
	m.Advice = "吞吐平稳"
	if !m.HasHalf || m.FirstAvg <= 0 {
		return
	}
	drop := (m.FirstAvg - m.LastAvg) / m.FirstAvg
	if drop >= 0.5 {
		m.Level = "warn"
		m.Advice = "吞吐下降"
	}
}

// noteHalfRise 在峰值已经到关注线，且后半段平均高出前半段 50% 以上时追加说明。
func noteHalfRise(m *PGMetric) {
	if m.Level != "warn" && m.Level != "danger" {
		return
	}
	if !m.HasHalf || m.FirstAvg <= 0 || m.LastAvg <= m.FirstAvg*1.5 {
		return
	}
	m.Advice += "，后半段升高"
}

func pgSlots(active, total, max *float64, timedOut bool) *PGSlotStat {
	if active == nil && total == nil {
		s := &PGSlotStat{Level: "muted"}
		if timedOut {
			s.Reason = "查询超时"
			s.Advice = "查询超时"
		} else {
			s.Reason = "没有样本"
			s.Advice = "没有样本"
		}
		return s
	}
	s := &PGSlotStat{Known: true, Level: "ok", Advice: "复制槽正常"}
	if active != nil {
		s.Active = *active
	}
	if total != nil {
		s.Total = *total
	} else {
		s.Total = s.Active
	}
	if active == nil {
		s.Active = s.Total
	}
	if max != nil && *max >= 0 && !math.IsNaN(*max) {
		n := *max
		s.Max = &n
	}
	if active != nil && total != nil && s.Total > s.Active {
		s.Level = "warn"
		s.Advice = "有未激活复制槽，确认是否可删"
	}
	return s
}

func pickPGAdvice(storage *PGStorage, deadlocks, cpu, tps *PGMetric, slots *PGSlotStat) (string, string) {
	type part struct {
		level  string
		advice string
	}
	parts := []part{}
	if storage != nil {
		parts = append(parts, part{storage.Level, storage.Advice})
	}
	for _, m := range []*PGMetric{deadlocks, cpu, tps} {
		if m != nil {
			parts = append(parts, part{m.Level, m.Advice})
		}
	}
	if slots != nil {
		parts = append(parts, part{slots.Level, slots.Advice})
	}
	bestLevel, bestAdvice := "muted", "没有可分析的样本"
	bestRank := 99
	for _, p := range parts {
		if strings.TrimSpace(p.advice) == "" {
			continue
		}
		rank := pgLevelRank(p.level)
		if rank < bestRank {
			bestRank = rank
			bestLevel = p.level
			bestAdvice = p.advice
		}
	}
	return bestLevel, bestAdvice
}

func pgLevelRank(level string) int {
	switch level {
	case "danger":
		return 0
	case "warn":
		return 1
	case "ok":
		return 2
	default:
		return 3
	}
}

// SortPGItems 先按写满天数从短到长，再按 CPU 峰值、死锁峰值从高到低。
func SortPGItems(items []PGTrendItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		da, db := pgDaysKey(a), pgDaysKey(b)
		if da != db {
			return da < db
		}
		ca, cb := pgMax(a.CPU), pgMax(b.CPU)
		if ca != cb {
			return ca > cb
		}
		return pgMax(a.Deadlocks) > pgMax(b.Deadlocks)
	})
}

func pgDaysKey(it PGTrendItem) float64 {
	if it.Storage == nil || it.Storage.DaysToFull == nil {
		return math.Inf(1)
	}
	return *it.Storage.DaysToFull
}

func pgMax(m *PGMetric) float64 {
	if m == nil || !m.Known {
		return 0
	}
	return m.Max
}

// SummarizePG 统计危险台数，并分别点出写满、死锁和 CPU。
func SummarizePG(items []PGTrendItem, days int) PGTrendSummary {
	if days < 1 {
		days = 1
	}
	s := PGTrendSummary{Instances: len(items), Hosts: len(items)}
	for _, it := range items {
		if it.Level == "danger" {
			s.Danger++
		}
		if pgFullRisk(it.Storage) {
			s.FullRisk++
		}
		if metricHot(it.Deadlocks) {
			s.Deadlocks++
		}
		if metricHot(it.CPU) {
			s.CPUHigh++
		}
		if it.TPS != nil && strings.Contains(it.TPS.Advice, "吞吐下降") {
			s.Throughput++
		}
		if it.Slots != nil && it.Slots.Known && it.Slots.Total > it.Slots.Active {
			s.InactiveSlots++
		}
		if it.Storage != nil && it.Storage.Level == "muted" && !metricKnown(it.QPS) && !metricKnown(it.TPS) && !metricKnown(it.CPU) && !pgItemTimedOut(it) {
			s.NoSample++
		}
	}
	if s.Instances == 0 {
		s.Conclusions = []string{"当前选择没有 PostgreSQL 实例"}
		return s
	}
	if s.Danger > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台需要优先处理", s.Danger))
	}
	if s.FullRisk > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台按近 %d 天库文件增速有写满风险", s.FullRisk, days))
	}
	if s.Deadlocks > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台有死锁，查日志并统一加锁顺序", s.Deadlocks))
	}
	if s.CPUHigh > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台 CPU 峰值达到 85%% 或以上", s.CPUHigh))
	}
	if s.Throughput > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台后半段吞吐比前半段下降超过 50%%", s.Throughput))
	}
	if s.InactiveSlots > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台有未激活复制槽，确认是否可删", s.InactiveSlots))
	}
	if len(s.Conclusions) == 0 {
		s.Conclusions = []string{fmt.Sprintf("近 %d 天没有需要优先处理的实例", days)}
	}
	if s.NoSample > 0 {
		s.Conclusions = append(s.Conclusions, fmt.Sprintf("%d 台没有库大小样本", s.NoSample))
	}
	for _, it := range items {
		if pgItemTimedOut(it) {
			s.Conclusions = append(s.Conclusions, "有指标查询超时，报告只含已返回的数据")
			break
		}
	}
	return s
}

func pgItemTimedOut(it PGTrendItem) bool {
	if it.Storage != nil && strings.Contains(it.Storage.Advice, "查询超时") {
		return true
	}
	if it.Slots != nil && strings.Contains(it.Slots.Reason, "查询超时") {
		return true
	}
	for _, m := range []*PGMetric{it.QPS, it.TPS, it.Locks, it.Deadlocks, it.CPU, it.SlotWAL} {
		if m != nil && strings.Contains(m.Reason, "查询超时") {
			return true
		}
	}
	return false
}

func pgFullRisk(s *PGStorage) bool {
	if s == nil || !s.CapacityKnown {
		return false
	}
	if s.DaysToFull != nil && *s.DaysToFull <= 30 {
		return true
	}
	return s.Rate >= 80 && s.HasPrev && s.GrowthBytes > flatBytes(s.SizeBytes)
}

func metricHot(m *PGMetric) bool {
	return m != nil && m.Known && (m.Level == "warn" || m.Level == "danger")
}

func metricKnown(m *PGMetric) bool {
	return m != nil && m.Known
}

// PGReportTitle 是保存到历史报告的标题。
func PGReportTitle(domain, host, envName string, n int) string {
	scope := "全部 PostgreSQL"
	switch {
	case domain != "" && host != "":
		scope = domain + " / " + host
	case domain != "":
		scope = domain
	case host != "":
		scope = host
	}
	if envName != "" {
		scope += " · " + envName
	}
	return fmt.Sprintf("%s 智能分析（%d 台）", scope, n)
}

// PGIndex 把 Prometheus 的 instance 标签和云监控实例名对上平台实例。
type PGIndex struct {
	exact  map[string]string
	byHost map[string][]string
	names  map[string][]pgNameHit
}

type pgNameHit struct {
	id  string
	idc string
}

// Add 登记一台实例的地址和域名。同一主机上有多台时，不带库端口的标签不再自动归到其中一台。
func (idx *PGIndex) Add(id, host, domain string, port int) {
	if idx.exact == nil {
		idx.exact = map[string]string{}
		idx.byHost = map[string][]string{}
	}
	id = strings.TrimSpace(id)
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.TrimSpace(domain))
	put := func(k string) {
		if k == "" || id == "" {
			return
		}
		if prev, ok := idx.exact[k]; ok && prev != id {
			idx.exact[k] = ""
			return
		}
		idx.exact[k] = id
	}
	if host != "" && port > 0 {
		put(host + ":" + strconv.Itoa(port))
	}
	if domain != "" && port > 0 {
		put(domain + ":" + strconv.Itoa(port))
	}
	put(host)
	put(domain)
	if host != "" {
		idx.byHost[host] = appendUnique(idx.byHost[host], id)
	}
	if h := hostPart(domain); h != "" && h != host {
		idx.byHost[h] = appendUnique(idx.byHost[h], id)
	}
}

// AddName 登记云监控实例名。同名且环境对不上时不猜测。
func (idx *PGIndex) AddName(id, name, idc string) {
	if idx.names == nil {
		idx.names = map[string][]pgNameHit{}
	}
	name = strings.ToLower(strings.TrimSpace(name))
	id = strings.TrimSpace(id)
	if name == "" || id == "" {
		return
	}
	idx.names[name] = append(idx.names[name], pgNameHit{id: id, idc: strings.ToLower(strings.TrimSpace(idc))})
}

// LookupSeries 先按 instance，再按 domain，最后按 hostname。
func (idx *PGIndex) LookupSeries(instance, domain, hostname string) (string, bool) {
	if id, ok := idx.Lookup(instance); ok {
		return id, true
	}
	if id, ok := idx.Lookup(domain); ok {
		return id, true
	}
	return idx.Lookup(hostname)
}

// Lookup 按 instance 标签找实例。精确的 host:port、域名优先；exporter 端口只在该主机只有一台时归并。
func (idx *PGIndex) Lookup(label string) (string, bool) {
	if idx == nil {
		return "", false
	}
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" {
		return "", false
	}
	if id, ok := idx.exact[label]; ok && id != "" {
		return id, true
	}
	host := hostPart(label)
	if ids := idx.byHost[host]; len(ids) == 1 {
		return ids[0], true
	}
	return "", false
}

// LookupName 按云监控实例名找实例。给出 idc 且能对上时只用那一条。
func (idx *PGIndex) LookupName(name, idc string) (string, bool) {
	if idx == nil {
		return "", false
	}
	hits := idx.names[strings.ToLower(strings.TrimSpace(name))]
	if len(hits) == 0 {
		return "", false
	}
	idc = strings.ToLower(strings.TrimSpace(idc))
	if idc != "" {
		var matched []pgNameHit
		for _, h := range hits {
			if h.idc == idc {
				matched = append(matched, h)
			}
		}
		if len(matched) == 1 {
			return matched[0].id, true
		}
		if len(matched) > 1 {
			return "", false
		}
	}
	if len(hits) == 1 {
		return hits[0].id, true
	}
	return "", false
}

func hostPart(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	if i := strings.LastIndex(label, ":"); i > 0 {
		return label[:i]
	}
	return label
}

func appendUnique(ids []string, id string) []string {
	for _, cur := range ids {
		if cur == id {
			return ids
		}
	}
	return append(ids, id)
}
