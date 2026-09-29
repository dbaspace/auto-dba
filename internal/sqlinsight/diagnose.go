package sqlinsight

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"auto-dba/internal/awr"
	"auto-dba/internal/store"
)

// 预设场景标签
const (
	siTagPromo            = "promo"
	siTagExpectedLoadUp   = "expected_load_up"
	siTagExpectedLoadDown = "expected_load_down"
	siTagMaintenance      = "maintenance"
	siTagMigration        = "migration"
	siTagCapacityTest     = "capacity_test"
	siTagKnownIssue       = "known_issue"
)

var siKnownScenarioTags = map[string]struct{}{
	siTagPromo: {}, siTagExpectedLoadUp: {}, siTagExpectedLoadDown: {},
	siTagMaintenance: {}, siTagMigration: {}, siTagCapacityTest: {}, siTagKnownIssue: {},
}

type sqlInsightDiagnoseInput struct {
	Finding  *store.SqlInsightFindingRow
	Sections *awr.AwrSections
	Metrics  map[string]*awr.AwrMetric
	Scenario *sqlInsightScenarioContext
}

type diagnoserFn func(in *sqlInsightDiagnoseInput) *DeepAnalysis

func normalizeScenario(text string, tags []string) *sqlInsightScenarioContext {
	text = strings.TrimSpace(text)
	if len(text) > 1000 {
		text = text[:1000]
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(t string) {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			return
		}
		if _, ok := siKnownScenarioTags[t]; !ok {
			return
		}
		if _, ok := seen[t]; ok {
			return
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	for _, t := range tags {
		add(t)
	}
	lower := strings.ToLower(text)
	kw := []struct {
		keys []string
		tag  string
	}{
		{[]string{"大促", "促销", "营销", "promo"}, siTagPromo},
		{[]string{"压测", "capacity", "压测"}, siTagCapacityTest},
		{[]string{"迁移", "migration"}, siTagMigration},
		{[]string{"变更", "发布", "割接", "维护", "maintenance"}, siTagMaintenance},
		{[]string{"流量下降", "业务下跌", "预期下跌", "流量下跌", "load down"}, siTagExpectedLoadDown},
		{[]string{"流量上升", "预期上涨", "高峰", "负载上升", "load up"}, siTagExpectedLoadUp},
		{[]string{"已知问题", "排查中", "known"}, siTagKnownIssue},
	}
	for _, item := range kw {
		for _, k := range item.keys {
			if strings.Contains(text, k) || strings.Contains(lower, strings.ToLower(k)) {
				add(item.tag)
				break
			}
		}
	}
	if text == "" && len(out) == 0 {
		return &sqlInsightScenarioContext{}
	}
	sort.Strings(out)
	return &sqlInsightScenarioContext{Text: text, Tags: out}
}

func (s *sqlInsightScenarioContext) has(tag string) bool {
	if s == nil {
		return false
	}
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func (s *sqlInsightScenarioContext) loadUp() bool {
	return s.has(siTagPromo) || s.has(siTagExpectedLoadUp) || s.has(siTagCapacityTest)
}

func (s *sqlInsightScenarioContext) loadDown() bool {
	return s.has(siTagExpectedLoadDown)
}

func (s *sqlInsightScenarioContext) changeWindow() bool {
	return s.has(siTagMaintenance) || s.has(siTagMigration)
}



func fmtFloat(v *float64, prec int) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.*f", prec, *v)
}

func diagnoserFor(metricKey string) diagnoserFn {
	switch metricKey {
	case "conn_usage_ratio", "backends":
		return diagnoseConn
	case "cache_hit_ratio":
		return diagnoseCacheHit
	case "tps":
		return diagnoseTPS
	case "cpu_usage", "mem_usage", "disk_usage":
		return diagnoseResource
	case "slow_query_cnt":
		return diagnoseSlowQuery
	case "deadlocks_ps":
		return diagnoseDeadlocks
	case "replication_lag":
		return diagnoseReplicationLag
	default:
		return diagnoseGeneric
	}
}

// sqlInsightEnrichDeepAnalysis 写入 evidence.deep_analysis 并刷新 suggestion。
func enrichDeepAnalysis(f *store.SqlInsightFindingRow, rep *AWRReportView, scenario *sqlInsightScenarioContext) {
	if f == nil {
		return
	}
	sec, metrics := parseAWRSections(rep)
	in := &sqlInsightDiagnoseInput{Finding: f, Sections: sec, Metrics: metrics, Scenario: scenario}
	deep := diagnoserFor(f.MetricKey)(in)
	if deep == nil {
		return
	}
	if scenario != nil && (scenario.Text != "" || len(scenario.Tags) > 0) {
		if deep.ScenarioApplied == nil {
			deep.ScenarioApplied = &ScenarioApplied{}
		}
		deep.ScenarioApplied.Tags = append([]string{}, scenario.Tags...)
		deep.ScenarioApplied.UserText = scenario.Text
	}
	sug := suggestionFromDeep(deep)
	if sug != "" {
		f.Suggestion = sug
	}
	ev := map[string]any{}
	if len(f.Evidence) > 0 && string(f.Evidence) != "{}" {
		_ = json.Unmarshal(f.Evidence, &ev)
	}
	ev["deep_analysis"] = deep
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	f.Evidence = b
}

func suggestionFromDeep(deep *DeepAnalysis) string {
	if deep == nil {
		return ""
	}
	var parts []string
	if strings.TrimSpace(deep.Summary) != "" {
		parts = append(parts, strings.TrimSpace(deep.Summary))
	}
	recs := append([]DeepRecommendation{}, deep.Recommendations...)
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Priority < recs[j].Priority })
	for _, r := range recs {
		if len(parts) >= 4 {
			break
		}
		t := strings.TrimSpace(r.Title)
		if t == "" {
			continue
		}
		parts = append(parts, t)
	}
	return strings.Join(parts, "；")
}

func deepFromEvidence(ev json.RawMessage) *DeepAnalysis {
	if len(ev) == 0 || string(ev) == "{}" {
		return nil
	}
	var wrap struct {
		Deep *DeepAnalysis `json:"deep_analysis"`
	}
	if json.Unmarshal(ev, &wrap) != nil || wrap.Deep == nil {
		return nil
	}
	return wrap.Deep
}

func applyScenarioNote(deep *DeepAnalysis, effect, title, detail string) {
	if deep == nil {
		return
	}
	if deep.ScenarioApplied == nil {
		deep.ScenarioApplied = &ScenarioApplied{}
	}
	deep.ScenarioApplied.Effects = append(deep.ScenarioApplied.Effects, effect)
	deep.Diagnoses = append(deep.Diagnoses, DeepDiagnosis{
		Code: "scenario_explained", Severity: siSevInfo, Title: title, Detail: detail,
	})
}

func diagnoseConn(in *sqlInsightDiagnoseInput) *DeepAnalysis {
	m := in.Metrics
	usage := awrMetricStat(m["conn_usage_ratio"], "max")
	backends := m["backends"]
	var backendsAvg, maxConn *float64
	if backends != nil {
		backendsAvg = awrMetricStat(backends, "avg")
		// 自管报告把 max_connections 写入 backends.Max
		if backends.Max != nil {
			maxConn = backends.Max
		}
	}
	if maxConn == nil && usage != nil && backendsAvg != nil && *usage > 0.01 {
		est := *backendsAvg / *usage
		maxConn = &est
	}
	idle := awrMetricStat(m["idle_conns"], "avg")
	active := awrMetricStat(m["active_conns"], "max")
	idleEstimated := false
	if idle == nil && backendsAvg != nil && active != nil && *backendsAvg >= *active {
		est := *backendsAvg - *active
		idle = &est
		idleEstimated = true
	}

	deep := &DeepAnalysis{
		RelatedMetrics: []string{"backends", "idle_conns", "active_conns", "conn_usage_ratio"},
	}
	if usage != nil {
		deep.Observations = append(deep.Observations, DeepObservation{
			Key: "conn_usage_ratio", Label: "连接使用率", Value: fmtFloat(usage, 4), Note: "告警线约 0.80 / 0.95",
		})
	}
	if backendsAvg != nil || maxConn != nil {
		val := fmt.Sprintf("%s/%s", fmtFloat(backendsAvg, 0), fmtFloat(maxConn, 0))
		deep.Observations = append(deep.Observations, DeepObservation{
			Key: "backends", Label: "当前连接/上限", Value: val, Note: "自管上限取自 backends.Max（max_connections）",
		})
	}
	if idle != nil {
		note := "腾讯云 idle_conns"
		if idleEstimated {
			note = "估算：backends − active（数据可能不全）"
		}
		deep.Observations = append(deep.Observations, DeepObservation{
			Key: "idle_conns", Label: "空闲连接", Value: fmtFloat(idle, 0), Note: note,
		})
	}
	if active != nil {
		deep.Observations = append(deep.Observations, DeepObservation{
			Key: "active_conns", Label: "活跃连接", Value: fmtFloat(active, 0),
		})
	}

	idleRatio := 0.0
	hasIdleRatio := false
	if idle != nil && backendsAvg != nil && *backendsAvg > 0 {
		idleRatio = *idle / *backendsAvg
		hasIdleRatio = true
		deep.Observations = append(deep.Observations, DeepObservation{
			Key: "idle_ratio", Label: "空闲占比", Value: fmt.Sprintf("%.2f", idleRatio),
		})
	}

	nearCap := usage != nil && *usage >= 0.80
	idleHeavy := hasIdleRatio && idleRatio >= 0.5 && nearCap
	activeBurst := nearCap && hasIdleRatio && idleRatio < 0.3
	if !hasIdleRatio && nearCap {
		// 无 idle 数据时仍标近满
		deep.Diagnoses = append(deep.Diagnoses, DeepDiagnosis{
			Code: "near_cap", Severity: in.Finding.Severity, Title: "连接接近上限",
			Detail: "连接使用率偏高；缺少 idle/active 拆分，请结合云监控或会话视图确认是否空闲连接堆积。",
		})
	}
	if idleHeavy {
		deep.Diagnoses = append(deep.Diagnoses, DeepDiagnosis{
			Code: "idle_heavy", Severity: siSevWarning, Title: "空闲连接占比过高",
			Detail: fmt.Sprintf("空闲占比约 %.0f%%，优先治理应用/中间连接池空闲连接，而非盲目提高 max_connections。", idleRatio*100),
		})
	}
	if activeBurst {
		deep.Diagnoses = append(deep.Diagnoses, DeepDiagnosis{
			Code: "active_burst", Severity: in.Finding.Severity, Title: "活跃连接逼近上限",
			Detail: "活跃连接占比较高，可能是业务高峰或慢查询堆积；评估限流、扩容与慢 SQL。",
		})
	} else if nearCap && !idleHeavy && hasIdleRatio {
		deep.Diagnoses = append(deep.Diagnoses, DeepDiagnosis{
			Code: "near_cap", Severity: in.Finding.Severity, Title: "连接接近上限",
			Detail: "连接使用率偏高，请结合 idle/active 与长事务进一步判断。",
		})
	}

	longTxnN := 0
	if in.Sections != nil {
		longTxnN = len(in.Sections.LongTxn)
	}
	if longTxnN > 0 {
		deep.Diagnoses = append(deep.Diagnoses, DeepDiagnosis{
			Code: "long_txn", Severity: siSevWarning, Title: "存在长事务",
			Detail: fmt.Sprintf("当日 AWR 记录约 %d 条长事务，可能占用连接并阻塞清理。", longTxnN),
		})
		deep.Recommendations = append(deep.Recommendations, DeepRecommendation{
			Category: "action", Priority: 3, Title: "排查 idle in transaction / 长事务",
			Detail: "结合 AWR long_txn 与活动会话，终止异常事务或优化事务边界。",
		})
		deep.RelatedMetrics = append(deep.RelatedMetrics, "max_xact_duration")
	}

	// 参数建议
	if idleHeavy || (nearCap && !activeBurst) {
		detail := "先降低各应用连接池 max 总和（建议 ≤0.7×max_connections），缩短空闲超时。"
		if maxConn != nil {
			detail = fmt.Sprintf("当前上限约 %.0f；%s", *maxConn, detail)
		}
		deep.Recommendations = append(deep.Recommendations, DeepRecommendation{
			Category: "parameter", Priority: 1, Title: "勿贸然提高 max_connections", Detail: detail,
		})
	} else if activeBurst {
		detail := "在确认非慢 SQL/锁等待后，可评估临时提高 max_connections 或水平扩容只读。"
		if maxConn != nil {
			detail = fmt.Sprintf("当前上限约 %.0f；%s", *maxConn, detail)
		}
		deep.Recommendations = append(deep.Recommendations, DeepRecommendation{
			Category: "parameter", Priority: 2, Title: "评估连接上限与读写分离", Detail: detail,
		})
	} else {
		deep.Recommendations = append(deep.Recommendations, DeepRecommendation{
			Category: "parameter", Priority: 2, Title: "核对连接池与 max_connections",
			Detail: "确保各应用池大小之和显著低于 max_connections，并设置合理 idle timeout。",
		})
	}

	if idleHeavy || nearCap {
		deep.Recommendations = append(deep.Recommendations, DeepRecommendation{
			Category: "tool", Priority: 2, Title: "引入/调优 PgBouncer",
			Detail: "推荐 transaction 模式；池大小约 CPU×2～4；应用侧 Hikari/Druid 池总和建议 ≤0.7×max_connections。",
		})
	}

	// 场景校正：负载上升时弱化「泄漏」语气，但仍保留 idle_heavy
	if in.Scenario != nil && in.Scenario.loadUp() && nearCap {
		if idleHeavy {
			applyScenarioNote(deep, "conn_high_with_idle_under_promo",
				"高峰场景下仍见空闲堆积", "用户场景为负载上升/大促，但空闲占比仍高，优先查连接池配置，不宜仅归因于业务高峰。")
			deep.Summary = "连接使用率偏高且空闲占比大：即使用户场景为高峰，仍建议先治理连接池空闲连接"
		} else {
			applyScenarioNote(deep, "conn_high_treated_as_expected_load",
				"可能与用户场景（预期负载上升）相关", "连接升高可能符合大促/压测场景；请确认池容量与限流是否足够，告警仍需关注。")
			deep.Summary = "连接使用率偏高，可能与用户场景中的预期负载上升相关；请确认连接池与限流"
			deep.Recommendations = append([]DeepRecommendation{{
				Category: "action", Priority: 1, Title: "核对高峰期池容量与限流",
				Detail: "对照用户场景确认是否为预期高峰；必要时临时扩容池或启用 PgBouncer。",
			}}, deep.Recommendations...)
		}
	}

	if deep.Summary == "" {
		switch {
		case idleHeavy:
			deep.Summary = "连接使用率偏高且空闲占比大，优先治理连接池而非盲目扩容"
		case activeBurst:
			deep.Summary = "连接使用率偏高且活跃连接为主，关注业务高峰、慢 SQL 与限流"
		case nearCap:
			deep.Summary = "连接使用率偏高，建议核查空闲连接、连接池与 max_connections"
		default:
			deep.Summary = "连接相关异常，建议结合 backends / idle / 长事务继续排查"
		}
	}
	return deep
}

func diagnoseCacheHit(in *sqlInsightDiagnoseInput) *DeepAnalysis {
	hit := awrMetricStat(in.Metrics["cache_hit_ratio"], "min")
	deep := &DeepAnalysis{
		Summary:        "缓存命中率偏低，关注 shared_buffers、全表扫描与缺失索引",
		RelatedMetrics: []string{"cache_hit_ratio", "tup_fetched_ps", "tup_returned_ps"},
		Observations: []DeepObservation{{
			Key: "cache_hit_ratio", Label: "缓存命中率(min)", Value: fmtFloat(hit, 4), Note: "告警线约 0.95 / 0.90",
		}},
		Diagnoses: []DeepDiagnosis{{
			Code: "cache_miss", Severity: in.Finding.Severity, Title: "缓冲命中不足",
			Detail: "可能存在大表扫描、shared_buffers 偏小或工作集超出内存。",
		}},
		Recommendations: []DeepRecommendation{
			{Category: "parameter", Priority: 1, Title: "评估 shared_buffers / effective_cache_size", Detail: "结合主机内存与工作集大小调整，避免盲目加大。"},
			{Category: "action", Priority: 2, Title: "排查 Top SQL 全表扫描与缺失索引", Detail: "结合 AWR top_sql 定位高读 SQL。"},
		},
	}
	if in.Sections != nil && len(in.Sections.TopSQL) > 0 {
		deep.Observations = append(deep.Observations, DeepObservation{
			Key: "top_sql", Label: "Top SQL 条数", Value: fmt.Sprintf("%d", len(in.Sections.TopSQL)),
		})
	}
	return deep
}

func diagnoseTPS(in *sqlInsightDiagnoseInput) *DeepAnalysis {
	tps := awrMetricStat(in.Metrics["tps"], "avg")
	deep := &DeepAnalysis{
		Summary:        "TPS 异常，区分业务下跌与数据库故障/只读切换",
		RelatedMetrics: []string{"tps", "backends", "deadlocks_ps", "replication_lag"},
		Observations: []DeepObservation{{
			Key: "tps", Label: "TPS(avg)", Value: fmtFloat(tps, 2),
		}},
		Recommendations: []DeepRecommendation{
			{Category: "action", Priority: 1, Title: "核对业务流量与主库角色", Detail: "确认是否业务下跌、主库只读或故障切换。"},
			{Category: "action", Priority: 2, Title: "关联长事务/死锁/复制延迟", Detail: "查看 long_txn、deadlocks_ps、replication_lag 是否同期异常。"},
		},
	}
	if in.Scenario != nil && in.Scenario.loadDown() {
		applyScenarioNote(deep, "tps_drop_matches_expected_load_down",
			"符合用户场景（预期负载下跌）", "TPS 下跌可能与用户声明的业务/流量下降一致，请先核对业务侧是否符合预期。")
		deep.Summary = "TPS 下跌，可能符合用户场景中的预期负载下跌；请先核对业务是否符合预期"
		deep.Recommendations = append([]DeepRecommendation{{
			Category: "action", Priority: 1, Title: "按用户场景核对业务流量",
			Detail: "用户已声明预期负载下跌，优先确认业务监控是否同步下降，再排查库侧故障。",
		}}, deep.Recommendations...)
	}
	return deep
}

func diagnoseResource(in *sqlInsightDiagnoseInput) *DeepAnalysis {
	key := in.Finding.MetricKey
	val := awrMetricStat(in.Metrics[key], "max")
	title := map[string]string{"cpu_usage": "CPU", "mem_usage": "内存", "disk_usage": "磁盘"}[key]
	if title == "" {
		title = key
	}
	deep := &DeepAnalysis{
		Summary:        fmt.Sprintf("%s 使用率偏高，结合主机监控与 Top SQL 排查", title),
		RelatedMetrics: []string{key, "tps", "backends"},
		Observations: []DeepObservation{{
			Key: key, Label: title + "使用率", Value: fmtFloat(val, 4),
		}},
		Diagnoses: []DeepDiagnosis{{
			Code: "resource_pressure", Severity: in.Finding.Severity, Title: title + "资源压力",
			Detail: "可能由慢 SQL、连接风暴或主机容量不足引起。",
		}},
		Recommendations: []DeepRecommendation{
			{Category: "action", Priority: 1, Title: "结合 Top SQL / 主机监控定位", Detail: "确认是计算密集 SQL、内存膨胀还是磁盘打满。"},
			{Category: "action", Priority: 2, Title: "评估扩容或限流", Detail: "高峰可临时扩容；持续高压需优化 SQL 或拆分负载。"},
		},
	}
	if in.Scenario != nil && in.Scenario.loadUp() {
		applyScenarioNote(deep, "resource_high_under_expected_load",
			"可能与用户场景（预期负载上升）相关", title+"升高可能符合大促/压测窗口，请确认容量规划是否覆盖。")
	}
	return deep
}

func diagnoseSlowQuery(in *sqlInsightDiagnoseInput) *DeepAnalysis {
	v := awrMetricStat(in.Metrics["slow_query_cnt"], "max")
	deep := &DeepAnalysis{
		Summary:        "慢查询增多，定位 Top SQL 并优化或限流",
		RelatedMetrics: []string{"slow_query_cnt", "max_xact_duration", "cpu_usage"},
		Observations: []DeepObservation{{
			Key: "slow_query_cnt", Label: "慢查询数", Value: fmtFloat(v, 0),
		}},
		Recommendations: []DeepRecommendation{
			{Category: "action", Priority: 1, Title: "定位 Top SQL / 长事务", Detail: "结合 AWR top_sql、活动快照优化或终止异常会话。"},
		},
	}
	if in.Sections != nil && len(in.Sections.TopSQL) > 0 {
		deep.Observations = append(deep.Observations, DeepObservation{
			Key: "top_sql", Label: "Top SQL 条数", Value: fmt.Sprintf("%d", len(in.Sections.TopSQL)),
		})
	}
	return deep
}

func diagnoseDeadlocks(in *sqlInsightDiagnoseInput) *DeepAnalysis {
	v := awrMetricStat(in.Metrics["deadlocks_ps"], "max")
	return &DeepAnalysis{
		Summary:        "死锁速率偏高，排查交叉更新顺序与锁等待",
		RelatedMetrics: []string{"deadlocks_ps", "locks"},
		Observations: []DeepObservation{{
			Key: "deadlocks_ps", Label: "死锁速率", Value: fmtFloat(v, 4),
		}},
		Recommendations: []DeepRecommendation{
			{Category: "action", Priority: 1, Title: "统一事务内加锁顺序", Detail: "检查交叉更新表的顺序与长事务持锁。"},
			{Category: "action", Priority: 2, Title: "缩短事务、避免热行竞争", Detail: "减少大事务与热点行更新冲突。"},
		},
	}
}

func diagnoseReplicationLag(in *sqlInsightDiagnoseInput) *DeepAnalysis {
	v := awrMetricStat(in.Metrics["replication_lag"], "max")
	deep := &DeepAnalysis{
		Summary:        "复制延迟偏高，检查网络、复制槽与长事务",
		RelatedMetrics: []string{"replication_lag", "replication_slots"},
		Observations: []DeepObservation{{
			Key: "replication_lag", Label: "复制延迟(s)", Value: fmtFloat(v, 1),
		}},
		Recommendations: []DeepRecommendation{
			{Category: "action", Priority: 1, Title: "检查网络与复制槽", Detail: "确认槽是否堆积、备库是否追不上。"},
			{Category: "action", Priority: 2, Title: "排查主库长事务", Detail: "长事务会阻碍 monotony / 延迟清理。"},
		},
	}
	if in.Scenario != nil && in.Scenario.changeWindow() {
		applyScenarioNote(deep, "lag_during_change_window",
			"可能与变更/迁移窗口相关", "用户场景为变更或迁移，复制延迟可能短暂升高，请核对窗口是否重叠。")
		deep.Recommendations = append([]DeepRecommendation{{
			Category: "action", Priority: 1, Title: "核对变更/迁移窗口是否重叠",
			Detail: "对照用户场景时间，确认延迟是否为窗口内预期现象。",
		}}, deep.Recommendations...)
	}
	return deep
}

func diagnoseGeneric(in *sqlInsightDiagnoseInput) *DeepAnalysis {
	sug := defaultSuggestion(in.Finding.MetricKey)
	return &DeepAnalysis{
		Summary: sug,
		Observations: []DeepObservation{{
			Key: in.Finding.MetricKey, Label: in.Finding.Title, Value: fmtFloat(in.Finding.Value, 4),
		}},
		Recommendations: []DeepRecommendation{
			{Category: "action", Priority: 1, Title: sug, Detail: "结合 AWR 与 SQL 洞察详情进一步排查。"},
		},
	}
}

func mergeDeepActions(narr *Narrative, findings []*store.SqlInsightFindingRow, scenario *sqlInsightScenarioContext) {
	if narr == nil {
		return
	}
	seen := map[string]struct{}{}
	for _, a := range narr.Actions {
		seen[a.Action] = struct{}{}
	}
	prio := len(narr.Actions) + 1
	if scenario != nil && (scenario.Text != "" || len(scenario.Tags) > 0) {
		msg := "已纳入用户业务场景判断"
		if scenario.Text != "" {
			msg = "用户场景：" + truncateRunes(scenario.Text, 80)
		} else if len(scenario.Tags) > 0 {
			msg = "用户场景标签：" + strings.Join(scenario.Tags, ",")
		}
		if _, ok := seen[msg]; !ok {
			narr.RootCauses = append([]string{msg}, narr.RootCauses...)
			if len(narr.RootCauses) > 8 {
				narr.RootCauses = narr.RootCauses[:8]
			}
			narr.Actions = append([]NarrativeAct{{
				Priority: 1, Action: "对照用户场景核对业务是否符合预期", RelatedMetrics: nil,
			}}, narr.Actions...)
			for i := range narr.Actions {
				narr.Actions[i].Priority = i + 1
			}
			seen[msg] = struct{}{}
			prio = len(narr.Actions) + 1
		}
		if scenario.loadDown() || scenario.loadUp() {
			narr.Confidence = "medium"
		}
	}
	for _, f := range findings {
		deep := deepFromEvidence(f.Evidence)
		if deep == nil {
			continue
		}
		recs := append([]DeepRecommendation{}, deep.Recommendations...)
		sort.SliceStable(recs, func(i, j int) bool { return recs[i].Priority < recs[j].Priority })
		for _, r := range recs {
			if r.Priority > 2 {
				continue
			}
			act := strings.TrimSpace(r.Title)
			if act == "" {
				continue
			}
			if _, ok := seen[act]; ok {
				continue
			}
			seen[act] = struct{}{}
			narr.Actions = append(narr.Actions, NarrativeAct{
				Priority: prio, Action: act, RelatedMetrics: []string{f.MetricKey},
			})
			prio++
			if len(narr.Actions) >= 12 {
				return
			}
		}
	}
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
