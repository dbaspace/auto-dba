package handler

import (
	"context"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"auto-dba/internal/instoverview"
	"auto-dba/internal/middleware"
	"auto-dba/internal/promclient"
	"auto-dba/internal/store"
)

const pgPromQueryTimeout = 20 * time.Second

// PGStorageTrend 提交 PostgreSQL 智能分析。与主机磁盘分析分开，不互相占用进行中的任务。
func (h *OverviewHandler) PGStorageTrend(c *gin.Context) {
	var req struct {
		Domain string `json:"domain"`
		Host   string `json:"host"`
		Env    string `json:"env"`
		Start  string `json:"start"`
		End    string `json:"end"`
	}
	_ = c.ShouldBindJSON(&req)
	now := time.Now()
	start, end, days, err := instoverview.ParseTrendWindow(req.Start, req.End, now)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	tenantID := middleware.TenantID(c)
	h.mu.Lock()
	if h.pgTrendJobs == nil {
		h.pgTrendJobs = map[string]*trendJob{}
	}
	for _, job := range h.pgTrendJobs {
		if job.TenantID == tenantID && job.Status == "running" {
			view := job.view()
			h.mu.Unlock()
			c.JSON(http.StatusOK, view)
			return
		}
	}
	job := &trendJob{ID: uuid.NewString(), TenantID: tenantID, Status: "running"}
	h.pgTrendJobs[job.ID] = job
	view := job.view()
	h.mu.Unlock()

	tid := effectiveListTenantID(h.Store, c)
	go h.runPGStorageTrend(job, trendRequest{
		ListTenant: tid,
		TenantID:   tenantID,
		UserID:     middleware.UserID(c),
		Username:   middleware.Username(c),
		Domain:     strings.TrimSpace(req.Domain),
		Host:       store.HostAddrKey(req.Host),
		Env:        strings.TrimSpace(req.Env),
		Start:      start,
		End:        end,
		Days:       days,
	})
	c.JSON(http.StatusOK, view)
}

// PGStorageTrendJob 返回 PostgreSQL 智能分析进度。完成后带 report_id。
func (h *OverviewHandler) PGStorageTrendJob(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	h.mu.Lock()
	defer h.mu.Unlock()
	job := h.pgTrendJobs[id]
	if job == nil || job.TenantID != middleware.TenantID(c) {
		c.JSON(http.StatusNotFound, gin.H{"message": "分析任务不存在"})
		return
	}
	c.JSON(http.StatusOK, job.view())
}

func (h *OverviewHandler) runPGStorageTrend(job *trendJob, req trendRequest) {
	fail := func(msg string) {
		h.mu.Lock()
		job.Status = "error"
		job.Message = msg
		h.mu.Unlock()
	}
	rows, err := h.listRows(req.ListTenant, req.UserID, "postgres", req.Env, "")
	if err != nil {
		fail(err.Error())
		return
	}
	targets := make([]overviewRow, 0, len(rows))
	for _, row := range rows {
		if req.Domain != "" && !strings.EqualFold(strings.TrimSpace(row.Domain), req.Domain) {
			continue
		}
		if req.Host != "" && store.HostAddrKey(row.Host) != req.Host {
			continue
		}
		if row.Sample == nil {
			row.Sample = &instoverview.Sample{}
		}
		targets = append(targets, row)
	}
	h.attachPostgresCloudDisk(targets)

	endByID := map[string]*float64{}
	startByID := map[string]*float64{}
	tpsStat := map[string]*instoverview.PGRateStat{}
	qpsStat := map[string]*instoverview.PGRateStat{}
	locksByID := map[string][]instoverview.PGPoint{}
	deadByID := map[string][]instoverview.PGPoint{}
	cpuStat := map[string]*instoverview.PGRateStat{}
	var endTimeout, startTimeout, tpsTimeout, qpsTimeout, locksTimeout, deadTimeout, cpuTimeout bool
	var slotTimeout, slotWALTimeout bool
	cloudEnd := map[string]*float64{}
	cloudStart := map[string]*float64{}
	slotActive := map[string]*float64{}
	slotTotal := map[string]*float64{}
	slotMax := map[string]*float64{}
	slotWALByID := map[string][]instoverview.PGPoint{}
	var rateHits []instoverview.RateHit
	if len(targets) > 0 {
		var idx instoverview.PGIndex
		addrs := make([]string, 0, len(targets)*2)
		names := make([]string, 0, len(targets)*2)
		for _, row := range targets {
			idx.Add(row.ID, row.Host, row.Domain, row.Port)
			idx.AddName(row.ID, row.Name, row.IDC)
			if hst := store.HostAddrKey(row.Host); hst != "" {
				addrs = append(addrs, hst)
			}
			if dom := store.HostAddrKey(row.Domain); dom != "" {
				addrs = append(addrs, dom)
				names = append(names, dom)
			}
			if name := strings.TrimSpace(row.Name); name != "" {
				names = append(names, name)
			}
		}
		instSel := instoverview.PGInstanceSelector(addrs)
		domSel := instoverview.PGDomainSelector(addrs)
		nameSel := instoverview.PGNameSelector(names)
		exporter := func(build func(string) string) string {
			return instoverview.PGEither(instSel, domSel, build)
		}
		if instSel != "" || nameSel != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			cli, err := h.promClient()
			if err != nil || cli == nil {
				fail("未配置 Prometheus（全局参数 awr_prometheus_base_url），无法分析磁盘增长")
				return
			}
			step := instoverview.PGQueryStep(req.Start, req.End)
			endSize, timedOut, err := pgInstant(ctx, cli, "库大小", exporter(instoverview.PGSizeExpr), req.End)
			if err != nil {
				fail(pgPromFailMsg(err))
				return
			}
			endTimeout = timedOut
			startSize, timedOut, err := pgInstant(ctx, cli, "库大小对照", exporter(instoverview.PGSizeExpr), req.Start)
			if err != nil {
				fail(pgPromFailMsg(err))
				return
			}
			startTimeout = timedOut
			qpsStat, tpsStat, qpsTimeout, tpsTimeout, err = pgLoadGrafanaRates(ctx, cli, targets, req.Start, req.End)
			if err != nil {
				fail(pgPromFailMsg(err))
				return
			}
			locks, timedOut, err := pgRange(ctx, cli, "锁", exporter(instoverview.PGLocksExpr), req.Start, req.End, step)
			if err != nil {
				fail(pgPromFailMsg(err))
				return
			}
			locksTimeout = timedOut
			dead, timedOut, err := pgRange(ctx, cli, "死锁", exporter(instoverview.PGDeadlocksExpr), req.Start, req.End, step)
			if err != nil {
				fail(pgPromFailMsg(err))
				return
			}
			deadTimeout = timedOut
			cpuStat, cpuTimeout, err = pgLoadCPU(ctx, cli, nameSel, &idx, req.Start, req.End)
			if err != nil {
				fail(pgPromFailMsg(err))
				return
			}
			endByID = pgInstantByInstance(endSize, &idx)
			startByID = pgInstantByInstance(startSize, &idx)
			locksByID = pgSeriesByInstance(locks, &idx)
			deadByID = pgSeriesByInstance(dead, &idx)
			rateHits = pgCurrentStorageRates(ctx, cli, nameSel)
			cloudEnd = pgCloudUsed(ctx, cli, nameSel, &idx, req.End)
			cloudStart = pgCloudUsed(ctx, cli, nameSel, &idx, req.Start)
			activeSamples, activeTO := pgOptionalInstant(ctx, cli, "复制槽", exporter(instoverview.PGSlotActiveExpr), req.End)
			totalSamples, totalTO := pgOptionalInstant(ctx, cli, "复制槽数", exporter(instoverview.PGSlotTotalExpr), req.End)
			maxSamples, maxTO := pgOptionalInstant(ctx, cli, "复制槽上限", exporter(instoverview.PGSlotMaxExpr), req.End)
			slotTimeout = activeTO || totalTO || maxTO
			slotActive = pgInstantByInstance(activeSamples, &idx)
			slotTotal = pgInstantByInstance(totalSamples, &idx)
			slotMax = pgInstantByInstance(maxSamples, &idx)
			wal, walTO := pgOptionalRange(ctx, cli, "槽日志", exporter(instoverview.PGSlotWALExpr), req.Start, req.End, step)
			if !walTO && len(wal) == 0 {
				wal, walTO = pgOptionalRange(ctx, cli, "槽日志", exporter(instoverview.PGSlotWALAltExpr), req.Start, req.End, step)
			}
			slotWALTimeout = walTO
			slotWALByID = pgSeriesByInstance(wal, &idx)
		}
	}

	items := make([]instoverview.PGTrendItem, 0, len(targets))
	for _, row := range targets {
		var capacity int64
		if row.Sample != nil && row.Sample.DiskKnown {
			capacity = row.Sample.DiskBytes
		}
		var currentRate *float64
		if v, ok := instoverview.PickStorageRate(row.Name, []string{row.IDC, row.EnvName}, rateHits); ok {
			currentRate = &v
		} else if v, ok := instoverview.PickStorageRate(row.Domain, []string{row.IDC, row.EnvName}, rateHits); ok {
			currentRate = &v
		}
		sizeEnd := endByID[row.ID]
		sizeStart := startByID[row.ID]
		endTO := endTimeout && sizeEnd == nil
		startTO := startTimeout && sizeStart == nil
		if sizeEnd == nil {
			if v := cloudEnd[row.ID]; v != nil {
				sizeEnd = v
				endTO = false
			}
		}
		if sizeStart == nil {
			if v := cloudStart[row.ID]; v != nil {
				sizeStart = v
				startTO = false
			}
		}
		items = append(items, instoverview.AnalyzePGInstance(instoverview.PGInstanceInput{
			ID: row.ID, Name: row.Name, App: row.App, Domain: row.Domain, Host: row.Host, EnvName: row.EnvName,
			Days: req.Days, Start: req.Start, End: req.End, Capacity: capacity, CurrentRate: currentRate,
			SizeEnd: sizeEnd, SizeStart: sizeStart,
			SizeEndTimeout: endTO, SizeStartTimeout: startTO,
			QPSStat: qpsStat[row.ID], TPSStat: tpsStat[row.ID], Locks: locksByID[row.ID],
			Deadlocks: deadByID[row.ID], CPUStat: cpuStat[row.ID],
			QPSTimeout: qpsTimeout, TPSTimeout: tpsTimeout, LocksTimeout: locksTimeout,
			DeadlocksTimeout: deadTimeout, CPUTimeout: cpuTimeout,
			SlotActive: slotActive[row.ID], SlotTotal: slotTotal[row.ID], SlotMax: slotMax[row.ID],
			SlotTimeout: slotTimeout && slotActive[row.ID] == nil && slotTotal[row.ID] == nil,
			SlotWAL:     slotWALByID[row.ID], SlotWALTimeout: slotWALTimeout && len(slotWALByID[row.ID]) == 0,
		}))
	}
	instoverview.SortPGItems(items)
	summary := instoverview.SummarizePG(items, req.Days)
	envName := h.reportEnvName(req.Env)
	scope := map[string]any{
		"domain": req.Domain, "host": req.Host, "env": req.Env, "env_name": envName,
		"days": req.Days, "host_count": len(items),
		"start": req.Start.Format(time.RFC3339), "end": req.End.Format(time.RFC3339),
	}
	payload := map[string]any{
		"days": req.Days, "items": items, "summary": summary,
		"generated_at": req.End.Format(time.RFC3339),
		"start":        req.Start.Format(time.RFC3339),
		"end":          req.End.Format(time.RFC3339),
		"note":         "使用率是当前腾讯云磁盘使用率。库文件不含 WAL，写满天数按购买磁盘估算。QPS、TPS 按 Grafana 的 hostname 精确汇总。QPS、TPS、CPU 的峰值都是 1 分钟分辨率上的最大值。",
	}
	id, err := h.saveAnalysisReport(req.TenantID, req.UserID, req.Username, store.AnalysisKindPGStorage, instoverview.PGReportTitle(req.Domain, req.Host, envName, len(items)), summary.Conclusions, scope, payload)
	if err != nil {
		log.Printf("save pg storage report: %v", err)
		fail("报告未保存：" + err.Error())
		return
	}
	h.mu.Lock()
	job.Status = "done"
	job.ReportID = id
	h.mu.Unlock()
}

// pgLoadGrafanaRates 按每台实例自己的 hostname 计算 QPS/TPS。
// 面板表达式只服务一个 $db，不能把地址、域名、实例名 or 在一起再取较大值。
func pgLoadGrafanaRates(ctx context.Context, cli *promclient.Client, targets []overviewRow, start, end time.Time) (map[string]*instoverview.PGRateStat, map[string]*instoverview.PGRateStat, bool, bool, error) {
	qps := map[string]*instoverview.PGRateStat{}
	tps := map[string]*instoverview.PGRateStat{}
	if cli == nil || len(targets) == 0 || !end.After(start) {
		return qps, tps, false, false, nil
	}
	rows := make([]instoverview.PGHostCandidate, 0, len(targets))
	var all []string
	for _, row := range targets {
		names := instoverview.PGHostnameCandidates(row.Name, row.Domain, store.HostAddrKey(row.Host))
		rows = append(rows, instoverview.PGHostCandidate{ID: row.ID, Names: names})
		all = append(all, names...)
	}
	samples, timedOut, err := pgInstant(ctx, cli, "TPS主机", instoverview.PGTPSByHostname(all), end)
	if err != nil {
		return nil, nil, false, false, err
	}
	if timedOut {
		return qps, tps, true, true, nil
	}
	present := map[string]bool{}
	for _, sm := range samples {
		if h := strings.TrimSpace(sm.Metric["hostname"]); h != "" {
			present[h] = true
		}
	}
	chosen := instoverview.PickPGHostnames(rows, present)
	if len(chosen) == 0 {
		return qps, tps, false, false, nil
	}
	hosts := make([]string, 0, len(chosen))
	seen := map[string]struct{}{}
	for _, h := range chosen {
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		hosts = append(hosts, h)
	}
	qpsInner := instoverview.PGQPSByHostname(hosts)
	tpsInner := instoverview.PGTPSByHostname(hosts)
	qpsMax, qpsMaxTO, err := pgInstant(ctx, cli, "QPS峰值", instoverview.PGOverTime(qpsInner, "max_over_time", start, end), end)
	if err != nil {
		return nil, nil, false, false, err
	}
	qpsAvg, qpsAvgTO, err := pgInstant(ctx, cli, "QPS平均", instoverview.PGOverTime(qpsInner, "avg_over_time", start, end), end)
	if err != nil {
		return nil, nil, false, false, err
	}
	tpsMax, tpsMaxTO, err := pgInstant(ctx, cli, "TPS峰值", instoverview.PGOverTime(tpsInner, "max_over_time", start, end), end)
	if err != nil {
		return nil, nil, false, false, err
	}
	tpsAvg, tpsAvgTO, err := pgInstant(ctx, cli, "TPS平均", instoverview.PGOverTime(tpsInner, "avg_over_time", start, end), end)
	if err != nil {
		return nil, nil, false, false, err
	}
	var first, last []promclient.Sample
	var firstTO, lastTO bool
	mid := start.Add(end.Sub(start) / 2)
	if mid.After(start) && end.After(mid) {
		first, firstTO, err = pgInstant(ctx, cli, "TPS前半", instoverview.PGOverTime(tpsInner, "avg_over_time", start, mid), mid)
		if err != nil {
			return nil, nil, false, false, err
		}
		last, lastTO, err = pgInstant(ctx, cli, "TPS后半", instoverview.PGOverTime(tpsInner, "avg_over_time", mid, end), end)
		if err != nil {
			return nil, nil, false, false, err
		}
	}
	if !qpsMaxTO {
		qps = pgRateByHost(chosen, pgHostValues(qpsAvg), pgHostValues(qpsMax), nil, nil)
	}
	if !tpsMaxTO {
		tps = pgRateByHost(chosen, pgHostValues(tpsAvg), pgHostValues(tpsMax), pgHostValues(first), pgHostValues(last))
	}
	return qps, tps, qpsMaxTO || qpsAvgTO, tpsMaxTO || tpsAvgTO || firstTO || lastTO, nil
}

func pgHostValues(samples []promclient.Sample) map[string]float64 {
	out := map[string]float64{}
	for _, sm := range samples {
		h := strings.TrimSpace(sm.Metric["hostname"])
		if h == "" {
			continue
		}
		v, ok := promclient.InstantValue(sm)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			continue
		}
		if _, exists := out[h]; exists {
			continue
		}
		out[h] = v
	}
	return out
}

func pgRateByHost(chosen map[string]string, avg, max, first, last map[string]float64) map[string]*instoverview.PGRateStat {
	out := map[string]*instoverview.PGRateStat{}
	for id, host := range chosen {
		mx, okMax := max[host]
		av, okAvg := avg[host]
		if !okMax && !okAvg {
			continue
		}
		st := &instoverview.PGRateStat{Known: true, Max: mx, Avg: av}
		if !okAvg {
			st.Avg = mx
		}
		if !okMax {
			st.Max = av
		}
		if first != nil && last != nil {
			if f, ok1 := first[host]; ok1 {
				if l, ok2 := last[host]; ok2 {
					st.First = f
					st.Last = l
					st.HasHalf = true
				}
			}
		}
		out[id] = st
	}
	return out
}

// pgLoadCPU 用云监控 CPU 的 1 分钟最大值和平均。15 分钟采样会漏掉 Grafana 上的窄尖峰。
func pgLoadCPU(ctx context.Context, cli *promclient.Client, nameSel string, idx *instoverview.PGIndex, start, end time.Time) (map[string]*instoverview.PGRateStat, bool, error) {
	out := map[string]*instoverview.PGRateStat{}
	if cli == nil || nameSel == "" || idx == nil || !end.After(start) {
		return out, false, nil
	}
	inner := instoverview.PGCPUExpr(nameSel)
	maxSamples, maxTO, err := pgInstant(ctx, cli, "CPU峰值", instoverview.PGOverTime(inner, "max_over_time", start, end), end)
	if err != nil {
		return nil, false, err
	}
	avgSamples, avgTO, err := pgInstant(ctx, cli, "CPU平均", instoverview.PGOverTime(inner, "avg_over_time", start, end), end)
	if err != nil {
		return nil, false, err
	}
	var first, last []promclient.Sample
	var firstTO, lastTO bool
	mid := start.Add(end.Sub(start) / 2)
	if mid.After(start) && end.After(mid) {
		first, firstTO, err = pgInstant(ctx, cli, "CPU前半", instoverview.PGOverTime(inner, "avg_over_time", start, mid), mid)
		if err != nil {
			return nil, false, err
		}
		last, lastTO, err = pgInstant(ctx, cli, "CPU后半", instoverview.PGOverTime(inner, "avg_over_time", mid, end), end)
		if err != nil {
			return nil, false, err
		}
	}
	if !maxTO {
		out = pgCPUStats(idx, pgNameValues(avgSamples), pgNameValues(maxSamples), pgNameValues(first), pgNameValues(last))
	}
	return out, maxTO || avgTO || firstTO || lastTO, nil
}

func pgNameValues(samples []promclient.Sample) map[string]float64 {
	out := map[string]float64{}
	for _, sm := range samples {
		name := strings.ToLower(strings.TrimSpace(sm.Metric["d_b_instance_name"]))
		if name == "" {
			continue
		}
		idc := strings.ToLower(strings.TrimSpace(sm.Metric["idc"]))
		v, ok := promclient.InstantValue(sm)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			continue
		}
		k := name + "\x00" + idc
		if _, exists := out[k]; exists {
			continue
		}
		out[k] = v
	}
	return out
}

func pgCPUStats(idx *instoverview.PGIndex, avg, max, first, last map[string]float64) map[string]*instoverview.PGRateStat {
	keys := map[string]struct{}{}
	for k := range avg {
		keys[k] = struct{}{}
	}
	for k := range max {
		keys[k] = struct{}{}
	}
	out := map[string]*instoverview.PGRateStat{}
	best := map[string]float64{}
	for k := range keys {
		name, idc, _ := strings.Cut(k, "\x00")
		id, ok := idx.LookupName(name, idc)
		if !ok {
			continue
		}
		mx, okMax := max[k]
		av, okAvg := avg[k]
		if !okMax && !okAvg {
			continue
		}
		st := &instoverview.PGRateStat{Known: true, Max: mx, Avg: av}
		if !okAvg {
			st.Avg = mx
		}
		if !okMax {
			st.Max = av
		}
		if f, ok1 := first[k]; ok1 {
			if l, ok2 := last[k]; ok2 {
				st.First = f
				st.Last = l
				st.HasHalf = true
			}
		}
		if prev, ok := best[id]; ok && prev >= st.Max {
			continue
		}
		out[id] = st
		best[id] = st.Max
	}
	return out
}

func pgPromTimedOut(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	return strings.Contains(msg, "504") || strings.Contains(msg, "网关超时") || strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline")
}

func pgPromFailMsg(err error) string {
	msg := err.Error()
	if strings.HasPrefix(msg, "Prometheus") {
		return msg
	}
	return "Prometheus：" + msg
}

func pgInstant(ctx context.Context, cli *promclient.Client, label, promql string, ts time.Time) ([]promclient.Sample, bool, error) {
	if promql == "" || cli == nil {
		return nil, false, nil
	}
	qctx, cancel := context.WithTimeout(ctx, pgPromQueryTimeout)
	defer cancel()
	samples, err := cli.Query(qctx, promql, ts)
	if pgPromTimedOut(err) {
		log.Printf("pg prom %s: %v", label, err)
		return nil, true, nil
	}
	return samples, false, err
}

func pgRange(ctx context.Context, cli *promclient.Client, label, promql string, start, end time.Time, step time.Duration) ([]promclient.Sample, bool, error) {
	if promql == "" || cli == nil {
		return nil, false, nil
	}
	qctx, cancel := context.WithTimeout(ctx, pgPromQueryTimeout)
	defer cancel()
	samples, err := cli.QueryRange(qctx, promql, start, end, step)
	if pgPromTimedOut(err) {
		log.Printf("pg prom %s: %v", label, err)
		return nil, true, nil
	}
	return samples, false, err
}

const pgCloudGiB = 1073741824

func pgOptionalInstant(ctx context.Context, cli *promclient.Client, label, promql string, ts time.Time) ([]promclient.Sample, bool) {
	samples, timedOut, err := pgInstant(ctx, cli, label, promql, ts)
	if err != nil {
		log.Printf("pg prom %s: %v", label, err)
		return nil, false
	}
	return samples, timedOut
}

func pgOptionalRange(ctx context.Context, cli *promclient.Client, label, promql string, start, end time.Time, step time.Duration) ([]promclient.Sample, bool) {
	samples, timedOut, err := pgRange(ctx, cli, label, promql, start, end, step)
	if err != nil {
		log.Printf("pg prom %s: %v", label, err)
		return nil, false
	}
	return samples, timedOut
}

// pgCurrentStorageRates 取腾讯云磁盘使用率的最新瞬时值，单位是 0–100。
func pgCurrentStorageRates(ctx context.Context, cli *promclient.Client, nameSel string) []instoverview.RateHit {
	samples, _ := pgOptionalInstant(ctx, cli, "磁盘使用率", instoverview.PGStorageRateExpr(nameSel), time.Now())
	var hits []instoverview.RateHit
	for _, sm := range samples {
		v, ok := promclient.InstantValue(sm)
		if !ok || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		name := strings.TrimSpace(sm.Metric["d_b_instance_name"])
		if name == "" {
			continue
		}
		hits = append(hits, instoverview.RateHit{Name: name, IDC: sm.Metric["idc"], Rate: v})
	}
	return hits
}

// pgCloudUsed 把云监控的数据盘和日志盘（GB）加成字节。exporter 已有库大小时调用方不用这份。
func pgCloudUsed(ctx context.Context, cli *promclient.Client, nameSel string, idx *instoverview.PGIndex, ts time.Time) map[string]*float64 {
	out := map[string]*float64{}
	if nameSel == "" || cli == nil || idx == nil {
		return out
	}
	data, _ := pgOptionalInstant(ctx, cli, "云数据盘", instoverview.PGCloudFileExpr(nameSel, "qce_postgres_datafilesize_max"), ts)
	logs, _ := pgOptionalInstant(ctx, cli, "云日志盘", instoverview.PGCloudFileExpr(nameSel, "qce_postgres_logfilesize_max"), ts)
	byData := pgInstantByName(data, idx)
	byLog := pgInstantByName(logs, idx)
	seen := map[string]struct{}{}
	for id := range byData {
		seen[id] = struct{}{}
	}
	for id := range byLog {
		seen[id] = struct{}{}
	}
	for id := range seen {
		var sum float64
		var n int
		if v := byData[id]; v != nil {
			sum += *v
			n++
		}
		if v := byLog[id]; v != nil {
			sum += *v
			n++
		}
		if n == 0 {
			continue
		}
		b := sum * pgCloudGiB
		out[id] = &b
	}
	return out
}

func pgInstantByInstance(samples []promclient.Sample, idx *instoverview.PGIndex) map[string]*float64 {
	best := map[string]float64{}
	seen := map[string]bool{}
	for _, sm := range samples {
		id, ok := idx.LookupSeries(sm.Metric["instance"], sm.Metric["domain"], sm.Metric["hostname"])
		if !ok {
			continue
		}
		v, ok := promclient.InstantValue(sm)
		if !ok || v < 0 {
			continue
		}
		if !seen[id] || v > best[id] {
			best[id] = v
			seen[id] = true
		}
	}
	out := map[string]*float64{}
	for id, v := range best {
		n := v
		out[id] = &n
	}
	return out
}

func pgSeriesByInstance(samples []promclient.Sample, idx *instoverview.PGIndex) map[string][]instoverview.PGPoint {
	best := map[string][]instoverview.PGPoint{}
	score := map[string]float64{}
	for _, sm := range samples {
		id, ok := idx.LookupSeries(sm.Metric["instance"], sm.Metric["domain"], sm.Metric["hostname"])
		if !ok {
			continue
		}
		pts, max := pgPoints(sm)
		if len(pts) == 0 {
			continue
		}
		if cur, ok := score[id]; ok && cur >= max {
			continue
		}
		best[id] = pts
		score[id] = max
	}
	return best
}

func pgInstantByName(samples []promclient.Sample, idx *instoverview.PGIndex) map[string]*float64 {
	best := map[string]float64{}
	seen := map[string]bool{}
	for _, sm := range samples {
		id, ok := idx.LookupName(sm.Metric["d_b_instance_name"], sm.Metric["idc"])
		if !ok {
			continue
		}
		v, ok := promclient.InstantValue(sm)
		if !ok || v < 0 {
			continue
		}
		if !seen[id] || v > best[id] {
			best[id] = v
			seen[id] = true
		}
	}
	out := map[string]*float64{}
	for id, v := range best {
		n := v
		out[id] = &n
	}
	return out
}

func pgPoints(sm promclient.Sample) ([]instoverview.PGPoint, float64) {
	raw := promclient.SampleToPoints(sm)
	pts := make([]instoverview.PGPoint, 0, len(raw))
	max := 0.0
	for _, p := range raw {
		pts = append(pts, instoverview.PGPoint{T: p.T, V: p.V})
		if p.V > max {
			max = p.V
		}
	}
	return pts, max
}
