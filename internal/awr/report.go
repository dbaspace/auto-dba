package awr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"auto-dba/internal/pgdiag"
	"auto-dba/internal/store"
)

const awrErrNoPrometheusMsg = "awr: prometheus base_url 未配置（请在全局参数设置 awr_prometheus_base_url）"

var errAwrNoPrometheus = errors.New(awrErrNoPrometheusMsg)

type awrReplSlotInfo struct {
	MaxSlots      *float64
	InactiveNames []string
	Err           error
}

func (s *Service) runGenerateWithReporter(ctx context.Context, day time.Time, instanceID, host string, port int64, rep *awrGenerateReporter) (processed, written, failed int, err error) {
	cli, ok := s.newAwrPromClient()
	if !ok {
		return 0, 0, 0, errAwrNoPrometheus
	}
	targets, err := s.enumeratePostgresConnections()
	if err != nil {
		return 0, 0, 0, err
	}

	filtered := make([]connTarget, 0, len(targets))
	for _, t := range targets {
		if instanceID != "" && t.inst.ID != instanceID {
			continue
		}
		if host != "" && (t.host != host || (port > 0 && t.port != port)) {
			continue
		}
		filtered = append(filtered, t)
	}

	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	rep.setTotal(ctx, len(filtered))
	for _, t := range filtered {
		if e := rep.checkpoint(ctx); e != nil {
			return processed, written, failed, e
		}
		rep.instanceStart(ctx, connectionKey(t.host, t.port))
		processed++
		row := s.generateOneReport(ctx, cli, t, start, end)
		if e := s.Store.UpsertAWRReport(ctx, row); e != nil {
			failed++
			log.Printf("awr: upsert report failed host=%s port=%d: %v", t.host, t.port, e)
		} else {
			written++
		}
		rep.instanceDone(ctx, processed, written, failed)
	}
	return processed, written, failed, nil
}

func (s *Service) generateOneReport(ctx context.Context, cli *awrPromClient, t connTarget, start, end time.Time) *store.AWRReportRow {
	evalTS := end
	if now := time.Now().UTC(); evalTS.After(now) {
		evalTS = now
	}

	source, baseSel, triedQueries, probeErr := s.detectSource(ctx, cli, t)
	sectionDefs := awrSectionsForSource(source)

	hostEnabled := source == awrSourceSelfPG && s.hostMetricsEnabled()
	hostSel, hostOK := "", false
	if hostEnabled {
		hostSel, hostOK = s.discoverHostSelector(ctx, cli, baseSel, evalTS)
	}

	sections := &AwrSections{GeneratedAt: time.Now().UTC()}
	var warnings []string
	var errorMessage string
	if source == awrSourceUnknown {
		if probeErr != nil {
			errorMessage = "Prometheus 指标探测查询失败，可能为 Thanos store 暂不可用或网络超时，请重试或检查监控后端：" + probeErr.Error()
		} else {
			errorMessage = "未在 Prometheus 找到该实例指标；已尝试: " + strings.Join(triedQueries, ", ")
		}
		warnings = append(warnings, errorMessage)
	}

	for _, secDef := range sectionDefs {
		sec := &AwrSection{Name: secDef.Name, Title: secDef.Title}
		for _, m := range secDef.Metrics {
			if m.Scope == awrScopeHost && !hostEnabled {
				continue
			}
			metric := &AwrMetric{Key: m.Key, Title: m.Title, Unit: m.Unit}
			sel := baseSel
			if m.Scope == awrScopeHost {
				if !hostOK {
					metric.Error = "host selector unavailable (pg_up exporter_addr not found)"
					warnings = append(warnings, secDef.Name+"."+m.Key+": "+metric.Error)
					sec.Metrics = append(sec.Metrics, metric)
					continue
				}
				sel = hostSel
			}
			promql := awrRenderPromQL(m.PromQL, sel)
			if m.Instant {
				samples, e := cli.Query(ctx, promql, evalTS)
				if e != nil {
					metric.Error = e.Error()
					warnings = append(warnings, secDef.Name+"."+m.Key+": "+e.Error())
				} else if len(samples) > 0 {
					if v, ok := instantSampleValue(samples[0]); ok {
						metric.Last, metric.Avg, metric.Max, metric.Min = &v, &v, &v, &v
					}
				}
			} else {
				samples, e := cli.QueryRange(ctx, promql, start, evalTS, awrPromDefaultStep)
				if e != nil {
					metric.Error = e.Error()
					warnings = append(warnings, secDef.Name+"."+m.Key+": "+e.Error())
				} else if len(samples) > 0 {
					applySeriesStats(metric, promSampleToSeries(samples[0]))
				}
			}
			sec.Metrics = append(sec.Metrics, metric)
		}
		if len(sec.Metrics) == 0 {
			continue
		}
		sections.Sections = append(sections.Sections, sec)
	}

	mergeMaxConnectionsIntoBackends(sections)

	var cloudWarnings []string
	if source == awrSourceSelfPG && s.cloudMetricsEnabled() {
		if cloudSel, ok := s.discoverCloudSelector(ctx, cli, baseSel, evalTS); ok {
			sec := &AwrSection{Name: "cloud_resource", Title: "资源使用（云监控）"}
			for _, m := range awrCloudResourceMetrics {
				metric := &AwrMetric{Key: m.Key, Title: m.Title, Unit: m.Unit}
				promql := awrRenderPromQL(m.PromQL, cloudSel)
				samples, e := cli.QueryRange(ctx, promql, start, evalTS, awrPromDefaultStep)
				if e != nil {
					metric.Error = e.Error()
					cloudWarnings = append(cloudWarnings, "cloud_resource."+m.Key+": "+e.Error())
				} else if len(samples) > 0 {
					applySeriesStats(metric, promSampleToSeries(samples[0]))
				}
				sec.Metrics = append(sec.Metrics, metric)
			}
			if len(sec.Metrics) > 0 {
				sections.Sections = append([]*AwrSection{sec}, sections.Sections...)
			}
			if capBytes, ok := awrStorageCapacityBytes(sections); ok {
				if m, _ := FindMetric(sections, "db_size_total"); m != nil {
					m.Max = &capBytes
				}
			}
		}
	}

	var replInfo awrReplSlotInfo
	if source == awrSourceSelfPG {
		replInfo = s.fetchReplicationSlotInfo(ctx, t)
		applyReplicationSlotInfo(sections, replInfo)
	}

	sections.TopSQL = s.topSQLFromSnapshots(t.inst.ID, start, end, "count")
	sections.LongTxn = s.topSQLFromSnapshots(t.inst.ID, start, end, "max_duration")
	statusWarnCount := len(warnings)
	sections.Warnings = append(warnings, cloudWarnings...)

	prevSections := s.prevDaySections(ctx, t.host, t.port, start)
	anomalies, severity := s.evaluateAnomalies(sections, prevSections)

	if source == awrSourceSelfPG && len(replInfo.InactiveNames) > 0 {
		cnt := float64(len(replInfo.InactiveNames))
		thr := 1.0
		anomalies = append(anomalies, AwrAnomaly{
			Key: "replication_slots", Title: "活跃复制槽数量", Section: "replication",
			Severity: awrSeverityCritical, Kind: "threshold",
			Value: &cnt, Threshold: &thr, Unit: "slots",
			Message: fmt.Sprintf("存在 %d 个非活跃复制槽(active=f)：%s；可能导致 WAL 堆积，请及时处理",
				len(replInfo.InactiveNames), strings.Join(replInfo.InactiveNames, ", ")),
		})
		severity = awrMaxSeverity(severity, awrSeverityCritical)
	}

	anomaliesRaw, aErr := json.Marshal(anomalies)
	if aErr != nil || len(anomaliesRaw) == 0 {
		anomaliesRaw = json.RawMessage(`[]`)
	}

	status := "success"
	switch {
	case source == awrSourceUnknown:
		status = "failed"
	case statusWarnCount > 0:
		status = "partial"
	}
	raw, mErr := json.Marshal(sections)
	if mErr != nil {
		raw = json.RawMessage(`{}`)
	}

	return &store.AWRReportRow{
		ReportDate:   start,
		InstanceID:   t.inst.ID,
		InstanceName: t.inst.Name,
		Host:         t.host,
		Port:         t.port,
		Source:       source,
		PeriodStart:  start,
		PeriodEnd:    end,
		Status:       status,
		Severity:     severity,
		AnomalyCount: len(anomalies),
		Anomalies:    anomaliesRaw,
		Sections:     raw,
		ErrorMessage: errorMessage,
	}
}

func mergeMaxConnectionsIntoBackends(sections *AwrSections) {
	for _, sec := range sections.Sections {
		if sec.Name != "connections" {
			continue
		}
		var backendsM, maxConnM *AwrMetric
		for _, m := range sec.Metrics {
			switch m.Key {
			case "backends":
				backendsM = m
			case "max_connections":
				maxConnM = m
			}
		}
		if backendsM != nil && maxConnM != nil {
			for _, st := range []string{"last", "max", "avg"} {
				if v := MetricStat(maxConnM, st); v != nil {
					vv := *v
					backendsM.Max = &vv
					break
				}
			}
			kept := make([]*AwrMetric, 0, len(sec.Metrics))
			for _, m := range sec.Metrics {
				if m.Key != "max_connections" {
					kept = append(kept, m)
				}
			}
			sec.Metrics = kept
		}
		break
	}
}

func applyReplicationSlotInfo(sections *AwrSections, replInfo awrReplSlotInfo) {
	var replSec *AwrSection
	var activeM *AwrMetric
	for _, sec := range sections.Sections {
		if sec.Name == "replication" {
			replSec = sec
			for _, m := range sec.Metrics {
				if m.Key == "replication_slots" {
					activeM = m
					break
				}
			}
			break
		}
	}
	if replInfo.MaxSlots != nil && activeM != nil {
		v := *replInfo.MaxSlots
		activeM.Max = &v
	}
	if activeM == nil && replInfo.MaxSlots != nil {
		v := *replInfo.MaxSlots
		maxM := &AwrMetric{Key: "max_replication_slots", Title: "最大复制槽数", Unit: "slots",
			Avg: &v, Max: &v, Min: &v, Last: &v}
		if replSec != nil {
			replSec.Metrics = append(replSec.Metrics, maxM)
		} else {
			sections.Sections = append(sections.Sections, &AwrSection{
				Name: "replication", Title: "复制延迟 / 槽", Metrics: []*AwrMetric{maxM},
			})
		}
	}
}

func (s *Service) detectSource(ctx context.Context, cli *awrPromClient, t connTarget) (source string, selector string, tried []string, probeErr error) {
	var probeTS time.Time
	label := s.stringArg(awrPromDBLabelKey, "id")
	var selfCandidates []string
	if id := strings.TrimSpace(t.inst.ID); id != "" {
		selfCandidates = append(selfCandidates, fmt.Sprintf(`%s=%q`, label, id))
		if label != "id" {
			selfCandidates = append(selfCandidates, fmt.Sprintf(`id=%q`, id))
		}
	}
	hostSel := s.awrDBSelector(t.host)
	dup := false
	for _, c := range selfCandidates {
		if c == hostSel {
			dup = true
			break
		}
	}
	if !dup {
		selfCandidates = append(selfCandidates, hostSel)
	}
	if len(selfCandidates) == 0 {
		selfCandidates = []string{hostSel}
	}

	for _, sel := range selfCandidates {
		q := "pg_up{" + sel + "}"
		tried = append(tried, q)
		if samples, err := awrProbeQuery(ctx, cli, q, probeTS); err != nil {
			probeErr = err
		} else if len(samples) > 0 {
			return awrSourceSelfPG, sel, tried, nil
		}
	}

	// qce 探测保留，但生成侧 cloud 补充默认关闭。
	if name := strings.TrimSpace(t.inst.Name); name != "" {
		cloudSel := s.awrCloudSelector(name)
		q := "qce_postgres_cpu_max{" + cloudSel + "}"
		tried = append(tried, q)
		if samples, err := awrProbeQuery(ctx, cli, q, probeTS); err != nil {
			probeErr = err
		} else if len(samples) > 0 {
			return awrSourceTencentQCE, cloudSel, tried, nil
		}
	}
	return awrSourceUnknown, selfCandidates[0], tried, probeErr
}

func awrProbeQuery(ctx context.Context, cli *awrPromClient, promql string, ts time.Time) ([]promSample, error) {
	samples, err := cli.Query(ctx, promql, ts)
	if err == nil {
		return samples, nil
	}
	select {
	case <-ctx.Done():
		return samples, err
	case <-time.After(500 * time.Millisecond):
	}
	return cli.Query(ctx, promql, ts)
}

func (s *Service) prevDaySections(ctx context.Context, host string, port int64, day time.Time) *AwrSections {
	prevDate := dateOnlyUTC(day.AddDate(0, 0, -1))
	row, err := s.Store.GetAWRReport(ctx, store.AWRGetFilter{ReportDate: &prevDate, Host: host, Port: port})
	if err != nil || row == nil || len(row.Sections) == 0 {
		return nil
	}
	var sec AwrSections
	if err := json.Unmarshal(row.Sections, &sec); err != nil {
		return nil
	}
	return &sec
}

func (s *Service) discoverHostSelector(ctx context.Context, cli *awrPromClient, dbSel string, ts time.Time) (string, bool) {
	samples, err := cli.Query(ctx, "pg_up{"+dbSel+"}", ts)
	if err != nil || len(samples) == 0 {
		return "", false
	}
	addr := samples[0].Metric["exporter_addr"]
	if strings.TrimSpace(addr) == "" {
		addr = samples[0].Metric["instance"]
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", false
	}
	ip := addr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		ip = addr[:i]
	}
	if ip == "" {
		return "", false
	}
	return s.awrHostSelector(ip), true
}

func (s *Service) discoverCloudSelector(ctx context.Context, cli *awrPromClient, dbSel string, ts time.Time) (string, bool) {
	samples, err := cli.Query(ctx, "pg_up{"+dbSel+"}", ts)
	if err != nil || len(samples) == 0 {
		return "", false
	}
	m := samples[0].Metric
	if rid := strings.TrimSpace(m["pg"]); rid != "" {
		return fmt.Sprintf(`resource_id=%q`, rid), true
	}
	name := strings.TrimSpace(m["db"])
	if name == "" {
		name = strings.TrimSpace(m["hostname"])
	}
	if name == "" {
		return "", false
	}
	return s.awrCloudSelector(name), true
}

func (s *Service) fetchReplicationSlotInfo(ctx context.Context, t connTarget) awrReplSlotInfo {
	pass, err := s.decryptPassword(t.inst)
	if err != nil {
		return awrReplSlotInfo{Err: err}
	}
	conn, err := pgdiag.Open(t.inst, pass)
	if err != nil {
		return awrReplSlotInfo{Err: err}
	}
	defer conn.Close()
	qctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := conn.PingContext(qctx); err != nil {
		return awrReplSlotInfo{Err: err}
	}
	out := awrReplSlotInfo{}
	var setting string
	if err := conn.QueryRowContext(qctx,
		`SELECT setting FROM pg_settings WHERE name = 'max_replication_slots'`).Scan(&setting); err == nil {
		if v, e := strconv.ParseFloat(strings.TrimSpace(setting), 64); e == nil {
			out.MaxSlots = &v
		}
	}
	rows, err := conn.QueryContext(qctx, `SELECT slot_name FROM pg_replication_slots WHERE active = false`)
	if err != nil {
		out.Err = err
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			out.InactiveNames = append(out.InactiveNames, strings.TrimSpace(name))
		}
	}
	return out
}

func awrStorageCapacityBytes(sections *AwrSections) (float64, bool) {
	dbM, _ := FindMetric(sections, "db_size_total")
	ratioM, _ := FindMetric(sections, "disk_usage")
	if dbM == nil || dbM.Last == nil || ratioM == nil {
		return 0, false
	}
	used := *dbM.Last
	if used <= 0 {
		return 0, false
	}
	const minRatio = 0.01
	for _, stat := range []string{"last", "avg", "max"} {
		ratio := MetricStat(ratioM, stat)
		if ratio == nil || *ratio < minRatio {
			continue
		}
		capBytes := used / *ratio
		if math.IsNaN(capBytes) || math.IsInf(capBytes, 0) || capBytes <= 0 {
			continue
		}
		return capBytes, true
	}
	return 0, false
}

func applySeriesStats(m *AwrMetric, series []AwrSeriesPoint) {
	if len(series) == 0 {
		return
	}
	sum := 0.0
	mx := series[0].V
	mn := series[0].V
	for _, p := range series {
		sum += p.V
		if p.V > mx {
			mx = p.V
		}
		if p.V < mn {
			mn = p.V
		}
	}
	avg := sum / float64(len(series))
	last := series[len(series)-1].V
	m.Avg, m.Max, m.Min, m.Last = &avg, &mx, &mn, &last
	m.Series = downsampleSeries(series, awrMaxSeriesPoints)
}

func downsampleSeries(series []AwrSeriesPoint, max int) []AwrSeriesPoint {
	if max <= 0 || len(series) <= max {
		return series
	}
	step := (len(series) + max - 1) / max
	out := make([]AwrSeriesPoint, 0, max+1)
	for i := 0; i < len(series); i += step {
		out = append(out, series[i])
	}
	if last := series[len(series)-1]; len(out) == 0 || out[len(out)-1].T != last.T {
		out = append(out, last)
	}
	return out
}

func instantSampleValue(s promSample) (float64, bool) {
	if len(s.Value) != 2 {
		return 0, false
	}
	return toFloat(s.Value[1])
}

func (s *Service) topSQLFromSnapshots(instanceID string, start, end time.Time, orderBy string) []*AwrTopSQL {
	rows, _, err := s.Store.ListPGActivityStats(store.PGActivityStatFilter{
		InstanceID: instanceID,
		StartTime:  start.UTC().Format(time.RFC3339),
		EndTime:    end.UTC().Format(time.RFC3339),
		OrderBy:    orderBy,
		Limit:      awrTopSQLLimit,
	})
	if err != nil {
		log.Printf("awr: top sql from snapshots failed instance_id=%s: %v", instanceID, err)
		return nil
	}
	out := make([]*AwrTopSQL, 0, len(rows))
	for _, r := range rows {
		out = append(out, &AwrTopSQL{
			QueryHash:       r.QueryHash,
			QueryNormalized: r.QueryNormalized,
			SampleInfo:      r.SampleInfo,
			Count:           r.Count,
			AvgXactDuration: r.AvgXactDuration,
			MaxXactDuration: r.MaxXactDuration,
		})
	}
	return out
}

func awrGenerateDay(date string) time.Time {
	day := time.Now().UTC().AddDate(0, 0, -1)
	if t, ok := parseAWRDate(strings.TrimSpace(date)); ok {
		day = t
	}
	return day
}

func parseAWRDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.UTC); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func dateOnlyUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ListReports 分页列表。
func (s *Service) ListReports(ctx context.Context, q *ReportQuery) (*ReportListData, error) {
	cur := q.Current
	if cur < 1 {
		cur = 1
	}
	ps := q.PageSize
	if ps <= 0 {
		ps = 20
	}
	filter := store.AWRListFilter{
		InstanceID: q.InstanceID,
		Host:       q.Host,
		Port:       q.Port,
		Status:     q.Status,
		Offset:     (cur - 1) * ps,
		Limit:      ps,
	}
	if t, ok := parseAWRDate(q.ReportDate); ok {
		d := dateOnlyUTC(t)
		filter.ReportDate = &d
	}
	if t, ok := parseAWRDate(q.StartDate); ok {
		d := dateOnlyUTC(t)
		filter.StartDate = &d
	}
	if t, ok := parseAWRDate(q.EndDate); ok {
		d := dateOnlyUTC(t)
		filter.EndDate = &d
	}
	rows, total, err := s.Store.ListAWRReports(ctx, filter)
	if err != nil {
		return nil, err
	}
	list := make([]*ReportItem, 0, len(rows))
	for _, r := range rows {
		list = append(list, rowToItem(r))
	}
	return &ReportListData{List: list, Total: total, Current: cur, PageSize: ps}, nil
}

// DetailReport 单份详情。
func (s *Service) DetailReport(ctx context.Context, q *ReportDetailQuery) (*ReportDetail, error) {
	filter := store.AWRGetFilter{ID: q.ID, Host: q.Host, Port: q.Port}
	if t, ok := parseAWRDate(q.ReportDate); ok {
		d := dateOnlyUTC(t)
		filter.ReportDate = &d
	}
	row, err := s.Store.GetAWRReport(ctx, filter)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	sections := row.Sections
	if len(sections) == 0 {
		sections = json.RawMessage(`{}`)
	}
	return &ReportDetail{ReportItem: *rowToItem(row), Sections: sections}, nil
}

func rowToItem(r *store.AWRReportRow) *ReportItem {
	item := &ReportItem{
		ID: r.ID, ReportDate: r.ReportDate.Format("2006-01-02"),
		InstanceID: r.InstanceID, InstanceName: r.InstanceName,
		Host: r.Host, Port: r.Port, Source: r.Source,
		PeriodStart: r.PeriodStart, PeriodEnd: r.PeriodEnd,
		Status: r.Status, Severity: r.Severity, AnomalyCount: r.AnomalyCount,
		ErrorMessage: r.ErrorMessage, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if len(r.Anomalies) > 0 {
		var anomalies []AwrAnomaly
		if err := json.Unmarshal(r.Anomalies, &anomalies); err == nil {
			item.Anomalies = anomalies
		}
	}
	return item
}

// AnomalySummaryReport 异常汇总。
func (s *Service) AnomalySummaryReport(ctx context.Context, q *AnomalyQuery) (*AnomalySummary, error) {
	filter := store.AWRAnomalySummaryFilter{Severity: strings.TrimSpace(q.Severity), Limit: q.Limit}
	if t, ok := parseAWRDate(q.ReportDate); ok {
		d := dateOnlyUTC(t)
		filter.ReportDate = &d
	}
	if t, ok := parseAWRDate(q.StartDate); ok {
		d := dateOnlyUTC(t)
		filter.StartDate = &d
	}
	if t, ok := parseAWRDate(q.EndDate); ok {
		d := dateOnlyUTC(t)
		filter.EndDate = &d
	}
	if filter.ReportDate == nil && filter.StartDate == nil && filter.EndDate == nil {
		d := dateOnlyUTC(time.Now().UTC().AddDate(0, 0, -1))
		filter.ReportDate = &d
	}
	res, err := s.Store.AnomalySummaryAWR(ctx, filter)
	if err != nil {
		return nil, err
	}
	top := make([]*ReportItem, 0, len(res.TopInstances))
	for _, r := range res.TopInstances {
		top = append(top, rowToItem(r))
	}
	return &AnomalySummary{
		Total: res.Total, CriticalCount: res.CriticalCount, WarningCount: res.WarningCount,
		NoneCount: res.NoneCount, AnomalyTotal: res.AnomalyTotal, TopInstances: top,
	}, nil
}

// SummaryReport 全实例异常汇总。
func (s *Service) SummaryReport(ctx context.Context, q *SummaryQuery) (*SummaryData, error) {
	day := time.Now().UTC().AddDate(0, 0, -1)
	if q != nil {
		if t, ok := parseAWRDate(strings.TrimSpace(q.ReportDate)); ok {
			day = t
		}
	}
	d := dateOnlyUTC(day)
	res, err := s.Store.AnomalySummaryAWR(ctx, store.AWRAnomalySummaryFilter{
		ReportDate: &d, Limit: awrSummaryMaxDomains, IncludeFailed: true,
	})
	if err != nil {
		return nil, err
	}
	data := &SummaryData{
		ReportDate: d.Format("2006-01-02"),
		Total:      res.Total, CriticalCount: res.CriticalCount, WarningCount: res.WarningCount,
		NoneCount: res.NoneCount, FailedCount: res.FailedCount, AnomalyTotal: res.AnomalyTotal,
	}
	anomalyDomains := 0
	for _, r := range res.TopInstances {
		item := rowToItem(r)
		if item.Severity != "" && item.Severity != "none" {
			anomalyDomains++
		}
		data.Domains = append(data.Domains, &SummaryDomainItem{
			InstanceID: item.InstanceID, InstanceName: item.InstanceName,
			Host: item.Host, Port: item.Port, Source: item.Source,
			Severity: item.Severity, Status: item.Status, ErrorMessage: item.ErrorMessage,
			AnomalyCount: item.AnomalyCount, Anomalies: item.Anomalies,
		})
	}
	data.AnomalyDomainCount = anomalyDomains
	data.IssueDomainCount = len(data.Domains)
	data.AnomalyTypeStats = aggregateAnomalyTypes(data.Domains)
	return data, nil
}

func aggregateAnomalyTypes(domains []*SummaryDomainItem) []*AnomalyTypeStat {
	statsByKey := make(map[string]*AnomalyTypeStat)
	order := make([]string, 0)
	for _, dom := range domains {
		if dom == nil || len(dom.Anomalies) == 0 {
			continue
		}
		sevByKey := make(map[string]string)
		metaByKey := make(map[string]AwrAnomaly)
		for _, a := range dom.Anomalies {
			if a.Key == "" {
				continue
			}
			sevByKey[a.Key] = awrMaxSeverity(sevByKey[a.Key], a.Severity)
			if _, ok := metaByKey[a.Key]; !ok {
				metaByKey[a.Key] = a
			}
		}
		for key, sev := range sevByKey {
			st, ok := statsByKey[key]
			if !ok {
				m := metaByKey[key]
				st = &AnomalyTypeStat{Key: key, Title: m.Title, Section: m.Section, Unit: m.Unit}
				statsByKey[key] = st
				order = append(order, key)
			}
			st.Count++
			switch sev {
			case awrSeverityCritical:
				st.CriticalCount++
			case awrSeverityWarning:
				st.WarningCount++
			}
		}
	}
	stats := make([]*AnomalyTypeStat, 0, len(order))
	for _, k := range order {
		stats = append(stats, statsByKey[k])
	}
	sort.SliceStable(stats, func(i, j int) bool {
		if stats[i].Count != stats[j].Count {
			return stats[i].Count > stats[j].Count
		}
		if stats[i].CriticalCount != stats[j].CriticalCount {
			return stats[i].CriticalCount > stats[j].CriticalCount
		}
		return stats[i].Key < stats[j].Key
	})
	return stats
}

// CleanupReports 按保留天数清理。
func (s *Service) CleanupReports(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	before := dateOnlyUTC(time.Now().UTC().AddDate(0, 0, -retentionDays))
	return s.Store.DeleteAWRReportsBefore(ctx, before)
}
