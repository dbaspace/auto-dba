package sqlinsight

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/aiops"
	"auto-dba/internal/store"
)

func (s *Service) buildNarrative(ctx context.Context, day time.Time, host string, port int, findings []*store.SqlInsightFindingRow, noAWR bool) *Narrative {
	crit, warn, info := 0, 0, 0
	sev := siSevNone
	var causes []string
	var actions []NarrativeAct
	seenAct := map[string]struct{}{}
	for i, f := range findings {
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
		cause := f.Message
		if c, _ := s.Store.GetSqlInsightCaseByFingerprint(ctx, f.Fingerprint); c != nil && strings.TrimSpace(c.RootCauseTemplate) != "" {
			cause = renderTemplate(c.RootCauseTemplate, f)
		}
		if len(causes) < 8 {
			causes = append(causes, cause)
		}
		act := f.Suggestion
		if c, _ := s.Store.GetSqlInsightCaseByFingerprint(ctx, f.Fingerprint); c != nil && strings.TrimSpace(c.ActionTemplate) != "" {
			act = renderTemplate(c.ActionTemplate, f)
		}
		if _, ok := seenAct[act]; !ok && act != "" {
			seenAct[act] = struct{}{}
			actions = append(actions, NarrativeAct{
				Priority: i + 1, Action: act, RelatedMetrics: []string{f.MetricKey},
			})
		}
	}
	scope := "全部实例"
	if host != "" {
		scope = fmt.Sprintf("%s:%d", host, port)
		if port <= 0 {
			scope = host
		}
	}
	headline := fmt.Sprintf("%s %s 分析完成：严重 %d / 告警 %d / 提示 %d",
		day.Format("2006-01-02"), scope, crit, warn, info)
	conf := "medium"
	if crit+warn > 0 {
		conf = "high"
	}
	if noAWR {
		headline = fmt.Sprintf("%s %s 分析完成：指定日期无 AWR 报告", day.Format("2006-01-02"), scope)
		causes = []string{fmt.Sprintf("日期 %s 无 AWR 报告，请先在「每日 AWR」生成该日报告，或改选已有报告的日期后再分析", day.Format("2006-01-02"))}
		conf = "low"
	} else if len(findings) == 0 {
		headline = fmt.Sprintf("%s %s 分析完成：未发现异常", day.Format("2006-01-02"), scope)
		causes = []string{"当日 AWR 无 anomalies，且未触发已启用规则与基线偏离"}
		conf = "low"
	}
	return &Narrative{
		Headline: headline, OverallSeverity: sev, RootCauses: causes, Actions: actions,
		Confidence: conf, Source: "baseline_and_cases",
	}
}

func renderTemplate(tpl string, f *store.SqlInsightFindingRow) string {
	out := tpl
	out = strings.ReplaceAll(out, "{host}", f.Host)
	out = strings.ReplaceAll(out, "{metric}", f.MetricKey)
	out = strings.ReplaceAll(out, "{severity}", f.Severity)
	if f.Value != nil {
		out = strings.ReplaceAll(out, "{value}", fmt.Sprintf("%.4f", *f.Value))
	}
	if f.BaselineValue != nil {
		out = strings.ReplaceAll(out, "{p95}", fmt.Sprintf("%.4f", *f.BaselineValue))
	}
	return out
}

// FindingFeedback 反馈 confirm/dismiss。
func (s *Service) FindingFeedback(ctx context.Context, id string, req *FeedbackReq, operator string) error {
	fb := strings.TrimSpace(req.Feedback)
	if fb != "confirm" && fb != "dismiss" {
		return fmt.Errorf("feedback 须为 confirm 或 dismiss")
	}
	f, err := s.Store.GetSqlInsightFinding(ctx, id)
	if err != nil {
		return err
	}
	if operator == "" {
		operator = "manual"
	}
	if err := s.Store.UpdateSqlInsightFindingFeedback(ctx, id, fb, req.Note, operator); err != nil {
		return err
	}
	_ = s.Store.IncrSqlInsightCaseFeedback(ctx, f.Fingerprint, fb)
	return nil
}

// SeedSystemCases 幂等写入/刷新系统案例（含 PostgreSQL 知识库，见 aiops.PostgresKnowledgeCases）。
func (s *Service) SeedSystemCases(ctx context.Context) error {
	now := time.Now().UTC()
	var firstErr error
	for _, kc := range aiops.AllKnowledgeCases() {
		alertNames := aiops.JoinAlertNames(kc.AlertNames)
		if alertNames == "" {
			alertNames = aiops.SuggestedAlertNamesForMetric(kc.MetricKey)
		}
		row := &store.SqlInsightCaseRow{
			ID: "seed-" + kc.Fingerprint, Fingerprint: kc.Fingerprint,
			MetricKey: kc.MetricKey, Severity: kc.Severity, Direction: kc.Direction,
			Title: kc.Title, RootCauseTemplate: kc.RootCauseTemplate, ActionTemplate: kc.ActionTemplate,
			AlertNames: alertNames, HitCount: 0, System: true, LastSeenAt: &now,
		}
		existing, err := s.Store.GetSqlInsightCaseByFingerprint(ctx, kc.Fingerprint)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if existing != nil {
			if err := s.Store.PatchSqlInsightSystemCase(ctx, row); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := s.Store.UpsertSqlInsightCase(ctx, row); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SeedSystemRules 幂等写入系统规则。
func (s *Service) SeedSystemRules(ctx context.Context) error {
	type seed struct {
		id, name, metric, kind, dir string
		warn, crit                  float64
	}
	seeds := []seed{
		{"sys-rule-cpu_usage-th", "系统: CPU 使用率过高", "cpu_usage", siRuleThreshold, siDirAbove, 0.85, 1.0},
		{"sys-rule-mem_usage-th", "系统: 内存使用率过高", "mem_usage", siRuleThreshold, siDirAbove, 0.85, 0.95},
		{"sys-rule-disk_usage-th", "系统: 磁盘使用率过高", "disk_usage", siRuleThreshold, siDirAbove, 0.80, 0.90},
		{"sys-rule-conn_usage-th", "系统: 连接使用率过高", "conn_usage_ratio", siRuleThreshold, siDirAbove, 0.80, 0.95},
		{"sys-rule-cache_hit-th", "系统: 缓存命中率过低", "cache_hit_ratio", siRuleThreshold, siDirBelow, 0.95, 0.90},
		{"sys-rule-repl_lag-th", "系统: 复制延迟过高", "replication_lag", siRuleThreshold, siDirAbove, 30, 300},
		{"sys-rule-deadlocks-th", "系统: 死锁速率过高", "deadlocks_ps", siRuleThreshold, siDirAbove, 0.01, 0.1},
		{"sys-rule-slow_query-th", "系统: 慢查询数过多", "slow_query_cnt", siRuleThreshold, siDirAbove, 10, 100},
		{"sys-rule-tps-dod", "系统: TPS 环比下跌", "tps", siRuleDoD, siDirDrop, 0.5, 0.8},
		{"sys-rule-backends-dod", "系统: 连接数环比飙升", "backends", siRuleDoD, siDirSpike, 1.0, 2.0},
		{"sys-rule-slow_query-dod", "系统: 慢查询环比飙升", "slow_query_cnt", siRuleDoD, siDirSpike, 1.0, 3.0},
		{"sys-rule-deadlocks-dod", "系统: 死锁环比飙升", "deadlocks_ps", siRuleDoD, siDirSpike, 1.0, 3.0},
		{"sys-rule-max_xact-th", "系统: 最长事务过久", "max_xact_duration", siRuleThreshold, siDirAbove, 600, 3600},
	}
	for _, seed := range seeds {
		if existing, err := s.Store.GetSqlInsightRuleByID(ctx, seed.id); err == nil && existing != nil {
			continue
		}
		warn, crit := seed.warn, seed.crit
		_ = s.Store.InsertSqlInsightRule(ctx, &store.SqlInsightRuleRow{
			ID: seed.id, Name: seed.name, Description: "与 AWR 默认异常规则对齐的系统预置规则",
			MetricKey: seed.metric, RuleKind: seed.kind, Direction: seed.dir,
			WarnThreshold: &warn, CritThreshold: &crit,
			BaselineWindow: siWindow30d, ZScoreK: 3, Enabled: true, System: true,
			CreatedBy: "system", UpdatedBy: "system",
		})
	}
	return nil
}
