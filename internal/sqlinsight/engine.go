package sqlinsight

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"auto-dba/internal/awr"
	"auto-dba/internal/store"
)

type siMetricObs struct {
	Key   string
	Title string
	Unit  string
	Stat  string
	Value float64
}

func parseDay(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		// 默认分析「昨天」
		now := time.Now().In(time.Local)
		y := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -1)
		return y, nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("date 须为 YYYY-MM-DD")
	}
	return t, nil
}

func (s *Service) ExecuteRun(ctx context.Context, day time.Time, host string, port int, trigger, createdBy string, scenario *sqlInsightScenarioContext) (*store.SqlInsightRunRow, error) {
	if scenario == nil {
		scenario = &sqlInsightScenarioContext{}
	}
	tagsJSON, _ := json.Marshal(scenario.Tags)
	if len(scenario.Tags) == 0 {
		tagsJSON = []byte("[]")
	}
	runID := uuid.NewString()
	run := &store.SqlInsightRunRow{
		ID: runID, AnalysisDate: day, Host: host, Port: port,
		Status: siRunRunning, Severity: siSevNone, NarrativeStatus: siNarrativeSkipped,
		TriggerType: trigger, CreatedBy: createdBy,
		UserScenario: scenario.Text, ScenarioTags: tagsJSON,
	}
	if err := s.Store.InsertSqlInsightRun(ctx, run); err != nil {
		return nil, err
	}

	metrics, err := s.Store.ListSqlInsightEnabledMetrics(ctx)
	if err != nil {
		run.Status = siRunFailed
		run.ErrorMessage = err.Error()
		_ = s.Store.FinishSqlInsightRun(ctx, run)
		return nil, err
	}

	// 幂等种子系统规则/案例（冷启动无用户规则时也能出数）
	_ = s.SeedSystemRules(ctx)
	_ = s.SeedSystemCases(ctx)

	rules, err := s.Store.ListSqlInsightEnabledRules(ctx)
	if err != nil {
		run.Status = siRunFailed
		run.ErrorMessage = err.Error()
		_ = s.Store.FinishSqlInsightRun(ctx, run)
		return nil, err
	}

	// 若用户尚未配置指标，用内置目录作为观测字段
	metricDefs := metrics
	if len(metricDefs) == 0 {
		for _, bf := range builtinCatalog() {
			metricDefs = append(metricDefs, &store.SqlInsightMetricRow{
				MetricKey: bf.Key, Name: bf.Name, Source: bf.Source, BuiltinField: bf.Key,
				Unit: bf.Unit, Stat: bf.DefaultStat, Enabled: true,
			})
		}
	}

	awrReports, err := s.AWR.ListReportsForDay(ctx, day, host, port)
	if err != nil {
		run.Status = siRunFailed
		run.ErrorMessage = err.Error()
		_ = s.Store.FinishSqlInsightRun(ctx, run)
		return nil, err
	}
	noAWR := len(awrReports) == 0
	reports := awrReports
	if len(reports) == 0 {
		reports = s.instanceTargets(host, port)
	}

	var findings []*store.SqlInsightFindingRow
	partial := false
	for i := range reports {
		rep := &reports[i]
		obsList, obsErr := s.collectObservations(ctx, day, rep, metricDefs)
		if obsErr != nil {
			partial = true
			continue
		}
		for _, obs := range obsList {
			_ = s.Store.UpsertSqlInsightObservation(ctx, runID, day, rep.Host, int(rep.Port), obs.Key, obs.Value, obs.Unit)
			_ = s.refreshBaseline(ctx, rep.Host, int(rep.Port), obs.Key, siWindow7d)
			_ = s.refreshBaseline(ctx, rep.Host, int(rep.Port), obs.Key, siWindow30d)
		}
		// 先导入 AWR 已有 anomalies，再跑规则；按 metric|severity 去重后做深度诊断
		imported := importAWRAnomalies(runID, day, rep)
		fs := s.evalRulesForInstance(ctx, runID, day, rep, obsList, rules)
		merged := dedupeFindings(append(imported, fs...))
		for _, f := range merged {
			enrichDeepAnalysis(f, rep, scenario)
		}
		findings = append(findings, merged...)
	}

	if err := s.Store.InsertSqlInsightFindings(ctx, findings); err != nil {
		run.Status = siRunFailed
		run.ErrorMessage = err.Error()
		_ = s.Store.FinishSqlInsightRun(ctx, run)
		return nil, err
	}

	// 案例命中 + 模板叙事 + 深度建议/场景
	narr := s.buildNarrative(ctx, day, host, port, findings, noAWR)
	mergeDeepActions(narr, findings, scenario)
	nj, _ := json.Marshal(narr)
	run.NarrativeJSON = nj
	run.NarrativeText = narr.Headline
	run.NarrativeSource = narr.Source
	run.NarrativeStatus = siNarrativeSucceeded

	crit, warn, info := 0, 0, 0
	sev := siSevNone
	for _, f := range findings {
		switch f.Severity {
		case siSevCritical:
			crit++
			sev = siSevCritical
		case siSevWarning:
			warn++
			if sev != siSevCritical {
				sev = siSevWarning
			}
		default:
			info++
			if sev == siSevNone {
				sev = siSevInfo
			}
		}
		now := time.Now().UTC()
		_ = s.Store.UpsertSqlInsightCase(ctx, &store.SqlInsightCaseRow{
			ID: uuid.NewString(), Fingerprint: f.Fingerprint, MetricKey: f.MetricKey,
			Severity: f.Severity, Direction: "", Title: f.Title,
			RootCauseTemplate: f.Message, ActionTemplate: f.Suggestion,
			HitCount: 1, LastSeenAt: &now, System: false,
		})
	}
	run.FindingCount = len(findings)
	run.CriticalCount = crit
	run.WarningCount = warn
	run.InfoCount = info
	run.Severity = sev
	if partial && len(findings) > 0 {
		run.Status = siRunPartial
	} else if noAWR {
		run.Status = siRunSucceeded
		run.ErrorMessage = fmt.Sprintf("指定日期 %s 无 AWR 报告，请先生成 AWR 或改选有数据的日期", day.Format("2006-01-02"))
	} else {
		run.Status = siRunSucceeded
	}
	if err := s.Store.FinishSqlInsightRun(ctx, run); err != nil {
		return nil, err
	}
	out, err := s.Store.GetSqlInsightRun(ctx, runID)
	if err != nil {
		return run, nil
	}
	return out, nil
}

func (s *Service) loadAWRReports(ctx context.Context, day time.Time, host string, port int) ([]AWRReportView, error) {
	list, err := s.AWR.ListReportsForDay(ctx, day, host, port)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return list, nil
	}
	// AWR 未就绪时，回退到 postgres 实例列表，以便 pg_activity 指标仍可分析
	return s.instanceTargets(host, port), nil
}

func (s *Service) instanceTargets(host string, port int) []AWRReportView {
	insts, err := s.Store.ListInstances("", "")
	if err != nil {
		return nil
	}
	var out []AWRReportView
	for _, it := range insts {
		dt := strings.ToLower(strings.TrimSpace(it.DBType))
		if dt != "postgres" && dt != "postgresql" && dt != "pg" {
			continue
		}
		if strings.TrimSpace(host) != "" && !strings.EqualFold(it.Host, host) && !strings.EqualFold(it.DomainName, host) {
			continue
		}
		if port > 0 && int(it.Port) != port {
			continue
		}
		out = append(out, AWRReportView{
			Host: it.Host, Port: int64(it.Port), InstanceID: it.ID, InstanceName: it.Name,
		})
	}
	return out
}

func (s *Service) collectObservations(ctx context.Context, day time.Time, rep *AWRReportView, metrics []*store.SqlInsightMetricRow) ([]siMetricObs, error) {
	var sections *awr.AwrSections
	if len(rep.Sections) > 0 {
		sections = &awr.AwrSections{}
		_ = json.Unmarshal(rep.Sections, sections)
	}
	var out []siMetricObs
	seen := map[string]struct{}{}
	for _, m := range metrics {
		if m.ScopeHost != "" && !strings.EqualFold(m.ScopeHost, rep.Host) {
			continue
		}
		if m.ScopePort > 0 && m.ScopePort != int(rep.Port) {
			continue
		}
		field := m.BuiltinField
		if field == "" {
			field = m.MetricKey
		}
		if _, ok := seen[field]; ok {
			continue
		}
		bf := builtinByKey(field)
		src := m.Source
		if bf != nil {
			src = bf.Source
		}
		stat := m.Stat
		if stat == "" && bf != nil {
			stat = bf.DefaultStat
		}
		unit := m.Unit
		title := m.Name
		if title == "" && bf != nil {
			title = bf.Name
		}
		var val *float64
		switch src {
		case siSourceAWR:
			if sections == nil {
				continue
			}
			am, _ := findAWRMetric(sections, field)
			val = awrMetricStat(am, stat)
		case siSourcePgActivity:
			v, err := s.activityMetric(ctx, day, rep.Host, int(rep.Port), field)
			if err != nil {
				continue
			}
			val = &v
		default:
			continue
		}
		if val == nil {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, siMetricObs{Key: field, Title: title, Unit: unit, Stat: stat, Value: *val})
	}
	return out, nil
}

func (s *Service) activityMetric(ctx context.Context, day time.Time, host string, port int, key string) (float64, error) {
	instID := s.resolveInstanceID(host, port)
	if instID == "" {
		return 0, fmt.Errorf("instance not found for %s:%d", host, port)
	}
	start := day
	end := day.Add(24 * time.Hour)
	switch key {
	case "max_xact_duration":
		v, ok, err := s.Store.PGActivityDayMaxXact(instID, start, end)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, fmt.Errorf("no data")
		}
		return v, nil
	case "active_session_count":
		n, err := s.Store.PGActivityDayActiveCount(instID, start, end)
		if err != nil {
			return 0, err
		}
		return float64(n), nil
	default:
		return 0, fmt.Errorf("unsupported activity metric")
	}
}

func (s *Service) resolveInstanceID(host string, port int) string {
	insts, err := s.Store.ListInstances("", "")
	if err != nil {
		return ""
	}
	for _, it := range insts {
		if strings.EqualFold(it.Host, host) && int(it.Port) == port {
			return it.ID
		}
		if strings.EqualFold(it.DomainName, host) && int(it.Port) == port {
			return it.ID
		}
	}
	return ""
}

func (s *Service) evalRulesForInstance(ctx context.Context, runID string, day time.Time, rep *AWRReportView, obs []siMetricObs, rules []*store.SqlInsightRuleRow) []*store.SqlInsightFindingRow {
	obsMap := map[string]siMetricObs{}
	for _, o := range obs {
		obsMap[o.Key] = o
	}
	var out []*store.SqlInsightFindingRow
	for _, rule := range rules {
		o, ok := obsMap[rule.MetricKey]
		if !ok {
			continue
		}
		f := s.evalOneRule(ctx, runID, day, rep, o, rule)
		if f != nil {
			out = append(out, f)
		}
	}
	return out
}

func (s *Service) evalOneRule(ctx context.Context, runID string, day time.Time, rep *AWRReportView, o siMetricObs, rule *store.SqlInsightRuleRow) *store.SqlInsightFindingRow {
	switch rule.RuleKind {
	case siRuleThreshold:
		return evalThreshold(runID, day, rep, o, rule)
	case siRuleDoD:
		return s.evalDoD(ctx, runID, day, rep, o, rule)
	case siRuleBaseline:
		return s.evalBaseline(ctx, runID, day, rep, o, rule)
	default:
		return nil
	}
}

func evalThreshold(runID string, day time.Time, rep *AWRReportView, o siMetricObs, rule *store.SqlInsightRuleRow) *store.SqlInsightFindingRow {
	sev, th := compareThreshold(o.Value, rule.Direction, rule.WarnThreshold, rule.CritThreshold)
	if sev == siSevNone {
		return nil
	}
	msg := fmt.Sprintf("%s 当前值 %.4f（%s）触发 %s 阈值 %.4f", o.Title, o.Value, rule.Direction, sev, *th)
	sug := defaultSuggestion(o.Key)
	fp := fingerprint(o.Key, sev, rule.Direction)
	ev, _ := json.Marshal(map[string]any{"stat": o.Stat, "compare": "absolute", "direction": rule.Direction})
	val := o.Value
	return &store.SqlInsightFindingRow{
		ID: uuid.NewString(), RunID: runID, AnalysisDate: day, Host: rep.Host, Port: int(rep.Port),
		InstanceID: rep.InstanceID, InstanceName: rep.InstanceName, MetricKey: o.Key, RuleID: rule.ID, RuleKind: rule.RuleKind,
		Severity: sev, Title: o.Title + " 阈值异常", Message: msg, Value: &val, Threshold: th, Unit: o.Unit,
		Evidence: ev, Suggestion: sug, Fingerprint: fp,
	}
}

func compareThreshold(val float64, dir string, warn, crit *float64) (string, *float64) {
	hit := func(th float64) bool {
		switch dir {
		case siDirBelow:
			return val < th
		default: // above
			return val > th
		}
	}
	if crit != nil && hit(*crit) {
		return siSevCritical, crit
	}
	if warn != nil && hit(*warn) {
		return siSevWarning, warn
	}
	return siSevNone, nil
}

func (s *Service) evalDoD(ctx context.Context, runID string, day time.Time, rep *AWRReportView, o siMetricObs, rule *store.SqlInsightRuleRow) *store.SqlInsightFindingRow {
	prevDay := day.AddDate(0, 0, -1)
	prevList, err := s.AWR.ListReportsForDay(ctx, prevDay, rep.Host, int(rep.Port))
	if err != nil || len(prevList) == 0 || len(prevList[0].Sections) == 0 {
		return nil
	}
	prev := prevList[0]
	var sections awr.AwrSections
	if json.Unmarshal(prev.Sections, &sections) != nil {
		return nil
	}
	am, _ := findAWRMetric(&sections, o.Key)
	base := awrMetricStat(am, o.Stat)
	if base == nil || *base == 0 {
		return nil
	}
	delta := (o.Value - *base) / math.Abs(*base)
	var change float64
	switch rule.Direction {
	case siDirDrop:
		change = -delta // 下跌为正
		if change < 0 {
			return nil
		}
	default: // spike
		change = delta
		if change < 0 {
			return nil
		}
	}
	sev, th := compareThreshold(change, siDirAbove, rule.WarnThreshold, rule.CritThreshold)
	if sev == siSevNone {
		return nil
	}
	msg := fmt.Sprintf("%s 相对昨日变化 %.1f%%（昨 %.4f → 今 %.4f）", o.Title, change*100, *base, o.Value)
	val := o.Value
	fp := fingerprint(o.Key, sev, rule.Direction)
	ev, _ := json.Marshal(map[string]any{"compare": "dod", "baseline": *base, "delta_pct": change, "direction": rule.Direction})
	return &store.SqlInsightFindingRow{
		ID: uuid.NewString(), RunID: runID, AnalysisDate: day, Host: rep.Host, Port: int(rep.Port),
		InstanceID: rep.InstanceID, InstanceName: rep.InstanceName, MetricKey: o.Key, RuleID: rule.ID, RuleKind: rule.RuleKind,
		Severity: sev, Title: o.Title + " 环比异常", Message: msg, Value: &val, Threshold: th, BaselineValue: base, Unit: o.Unit,
		Evidence: ev, Suggestion: defaultSuggestion(o.Key), Fingerprint: fp,
	}
}

func (s *Service) evalBaseline(ctx context.Context, runID string, day time.Time, rep *AWRReportView, o siMetricObs, rule *store.SqlInsightRuleRow) *store.SqlInsightFindingRow {
	win := rule.BaselineWindow
	if win == "" {
		win = siWindow30d
	}
	b, err := s.Store.GetSqlInsightBaseline(ctx, rep.Host, int(rep.Port), o.Key, win)
	if err != nil || b == nil || b.SampleCount < siBaselineMinSamples {
		return nil
	}
	sev := siSevNone
	var th *float64
	var reason string
	// 优先 p95 偏离
	if b.P95 != nil {
		switch rule.Direction {
		case siDirBelow:
			if o.Value < *b.P95 { // for below metrics like cache_hit, use p50-ish; here treat warn if below historical p50 if available
				if b.P50 != nil && o.Value < *b.P50 {
					sev = siSevWarning
					th = b.P50
					reason = "低于历史中位数"
				}
			}
		default:
			if o.Value > *b.P95 {
				sev = siSevWarning
				th = b.P95
				reason = "高于历史 p95"
			}
			if b.P99 != nil && o.Value > *b.P99 {
				sev = siSevCritical
				th = b.P99
				reason = "高于历史 p99"
			}
		}
	}
	// Z-Score
	if b.Mean != nil && b.Stddev != nil && *b.Stddev > 1e-9 {
		z := (o.Value - *b.Mean) / *b.Stddev
		k := rule.ZScoreK
		if k <= 0 {
			k = 3
		}
		if rule.Direction == siDirBelow {
			z = -z
		}
		if z >= k*1.5 && sev != siSevCritical {
			sev = siSevCritical
			reason = fmt.Sprintf("Z-Score=%.2f", z)
		} else if z >= k && sev == siSevNone {
			sev = siSevWarning
			reason = fmt.Sprintf("Z-Score=%.2f", z)
		}
	}
	if sev == siSevNone {
		return nil
	}
	val := o.Value
	msg := fmt.Sprintf("%s 当前值 %.4f，%s（窗口 %s, n=%d）", o.Title, o.Value, reason, win, b.SampleCount)
	fp := fingerprint(o.Key, sev, "baseline")
	ev, _ := json.Marshal(map[string]any{
		"compare": "baseline", "window": win, "sample_count": b.SampleCount,
		"mean": b.Mean, "p95": b.P95, "p99": b.P99, "reason": reason,
	})
	return &store.SqlInsightFindingRow{
		ID: uuid.NewString(), RunID: runID, AnalysisDate: day, Host: rep.Host, Port: int(rep.Port),
		InstanceID: rep.InstanceID, InstanceName: rep.InstanceName, MetricKey: o.Key, RuleID: rule.ID, RuleKind: rule.RuleKind,
		Severity: sev, Title: o.Title + " 偏离基线", Message: msg, Value: &val, Threshold: th, BaselineValue: b.P95, Unit: o.Unit,
		Evidence: ev, Suggestion: defaultSuggestion(o.Key), Fingerprint: fp,
	}
}

func fingerprint(metricKey, severity, direction string) string {
	return strings.ToLower(strings.TrimSpace(metricKey) + "|" + severity + "|" + direction)
}

func defaultSuggestion(key string) string {
	switch key {
	case "cache_hit_ratio":
		return "检查 shared_buffers、热点全表扫描与缺失索引"
	case "conn_usage_ratio", "backends":
		return "检查连接池配置、空闲连接与突发业务"
	case "cpu_usage", "mem_usage", "disk_usage":
		return "结合主机监控排查资源瓶颈与慢 SQL"
	case "replication_lag":
		return "检查网络、复制槽与长事务阻塞"
	case "deadlocks_ps":
		return "排查交叉更新顺序与锁等待"
	case "slow_query_cnt", "max_xact_duration":
		return "定位 Top SQL / 长事务并优化或终止异常会话"
	case "tps":
		return "确认是否业务下跌或主库只读/故障切换"
	default:
		return "结合 AWR 与 SQL 洞察详情进一步排查"
	}
}

func runToDTO(r *store.SqlInsightRunRow, withNarrative bool) *RunItem {
	if r == nil {
		return nil
	}
	item := &RunItem{
		ID: r.ID, AnalysisDate: r.AnalysisDate.Format("2006-01-02"), Host: r.Host, Port: r.Port,
		Status: r.Status, Severity: r.Severity, FindingCount: r.FindingCount,
		CriticalCount: r.CriticalCount, WarningCount: r.WarningCount, InfoCount: r.InfoCount,
		ErrorMessage: r.ErrorMessage, UserScenario: r.UserScenario,
		NarrativeStatus: r.NarrativeStatus, NarrativeText: r.NarrativeText,
		NarrativeSource: r.NarrativeSource, TriggerType: r.TriggerType, CreatedBy: r.CreatedBy,
		StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, CreatedAt: r.CreatedAt,
	}
	if len(r.ScenarioTags) > 0 && string(r.ScenarioTags) != "[]" {
		var tags []string
		if json.Unmarshal(r.ScenarioTags, &tags) == nil {
			item.ScenarioTags = tags
		}
	}
	if withNarrative && len(r.NarrativeJSON) > 0 && string(r.NarrativeJSON) != "{}" {
		var n Narrative
		if json.Unmarshal(r.NarrativeJSON, &n) == nil {
			item.Narrative = &n
		}
	}
	return item
}

func findingToDTO(r *store.SqlInsightFindingRow) *FindingItem {
	item := &FindingItem{
		ID: r.ID, RunID: r.RunID, AnalysisDate: r.AnalysisDate.Format("2006-01-02"),
		Host: r.Host, Port: r.Port, InstanceID: r.InstanceID, InstanceName: r.InstanceName,
		MetricKey: r.MetricKey, RuleID: r.RuleID, RuleKind: r.RuleKind, Severity: r.Severity,
		Title: r.Title, Message: r.Message, Value: r.Value, Threshold: r.Threshold, BaselineValue: r.BaselineValue,
		Unit: r.Unit, Evidence: r.Evidence, Suggestion: r.Suggestion, Fingerprint: r.Fingerprint,
		Feedback: r.Feedback, FeedbackNote: r.FeedbackNote, CreatedAt: r.CreatedAt,
	}
	item.DeepAnalysis = deepFromEvidence(r.Evidence)
	return item
}

// --- baseline math (exported for tests via same package) ---

func (s *Service) refreshBaseline(ctx context.Context, host string, port int, metricKey, windowKey string) error {
	days := 30
	if windowKey == siWindow7d {
		days = 7
	}
	since := time.Now().In(time.Local).AddDate(0, 0, -days)
	vals, err := s.Store.ListSqlInsightObservationValues(ctx, host, port, metricKey, since)
	if err != nil {
		return err
	}
	b := computeBaseline(vals)
	b.Host = host
	b.Port = port
	b.MetricKey = metricKey
	b.WindowKey = windowKey
	return s.Store.UpsertSqlInsightBaseline(ctx, b)
}

func computeBaseline(vals []float64) *store.SqlInsightBaselineRow {
	b := &store.SqlInsightBaselineRow{SampleCount: len(vals)}
	if len(vals) == 0 {
		return b
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	mean := sum / float64(len(vals))
	b.Mean = &mean
	var variance float64
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	std := math.Sqrt(variance / float64(len(vals)))
	b.Stddev = &std
	p50 := percentile(sorted, 0.50)
	p95 := percentile(sorted, 0.95)
	p99 := percentile(sorted, 0.99)
	b.P50, b.P95, b.P99 = &p50, &p95, &p99
	mn, mx := sorted[0], sorted[len(sorted)-1]
	b.Min, b.Max = &mn, &mx
	last := vals[len(vals)-1]
	b.LastValue = &last
	return b
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	w := idx - float64(lo)
	return sorted[lo]*(1-w) + sorted[hi]*w
}


// sqlInsightImportAWRAnomalies 将 AWR 报告已落库的 anomalies 转为 findings（冷启动即时出数）。
func importAWRAnomalies(runID string, day time.Time, rep *AWRReportView) []*store.SqlInsightFindingRow {
	if rep == nil || len(rep.Anomalies) == 0 || string(rep.Anomalies) == "[]" {
		return nil
	}
	var anoms []awr.AwrAnomaly
	if err := json.Unmarshal(rep.Anomalies, &anoms); err != nil || len(anoms) == 0 {
		return nil
	}
	out := make([]*store.SqlInsightFindingRow, 0, len(anoms))
	for _, a := range anoms {
		f := findingFromAWRAnomaly(runID, day, rep, a)
		if f != nil {
			out = append(out, f)
		}
	}
	return out
}

func findingFromAWRAnomaly(runID string, day time.Time, rep *AWRReportView, a awr.AwrAnomaly) *store.SqlInsightFindingRow {
	sev := strings.TrimSpace(a.Severity)
	if sev != siSevWarning && sev != siSevCritical && sev != siSevInfo {
		return nil
	}
	key := strings.TrimSpace(a.Key)
	if key == "" {
		return nil
	}
	kind := strings.TrimSpace(a.Kind)
	if kind == "" {
		kind = "threshold"
	}
	dir := kind
	switch kind {
	case "dod":
		dir = siDirDrop // fingerprint 与案例种子对齐；spike 类在 message 中仍可见
		if strings.Contains(strings.ToLower(a.Message), "上涨") || strings.Contains(strings.ToLower(a.Message), "飙升") {
			dir = siDirSpike
		}
	case "threshold":
		dir = siDirAbove
		if strings.Contains(a.Message, "低于") || strings.Contains(strings.ToLower(a.Message), "below") {
			dir = siDirBelow
		}
		// 已知 below 指标
		if key == "cache_hit_ratio" {
			dir = siDirBelow
		}
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = key
	}
	msg := strings.TrimSpace(a.Message)
	if msg == "" {
		msg = fmt.Sprintf("%s 触发 AWR %s 异常", title, sev)
	}
	fp := fingerprint(key, sev, dir)
	ev, _ := json.Marshal(map[string]any{
		"source": "awr_import", "kind": kind, "section": a.Section, "delta_pct": a.DeltaPct,
	})
	return &store.SqlInsightFindingRow{
		ID: uuid.NewString(), RunID: runID, AnalysisDate: day,
		Host: rep.Host, Port: int(rep.Port), InstanceID: rep.InstanceID, InstanceName: rep.InstanceName,
		MetricKey: key, RuleID: "awr", RuleKind: siRuleKindAWRImport,
		Severity: sev, Title: title + "（AWR）", Message: msg,
		Value: a.Value, Threshold: a.Threshold, BaselineValue: a.Baseline, Unit: a.Unit,
		Evidence: ev, Suggestion: defaultSuggestion(key), Fingerprint: fp,
	}
}

// sqlInsightDedupeFindings 按 metric_key|severity 去重，保留先出现的（AWR 导入优先于规则重复项）。
func dedupeFindings(list []*store.SqlInsightFindingRow) []*store.SqlInsightFindingRow {
	seen := make(map[string]struct{}, len(list))
	out := make([]*store.SqlInsightFindingRow, 0, len(list))
	for _, f := range list {
		if f == nil {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(f.MetricKey) + "|" + strings.TrimSpace(f.Severity))
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, f)
	}
	return out
}
