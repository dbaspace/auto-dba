package sqlinsight

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// SeedSystemCases 幂等写入系统案例。
func (s *Service) SeedSystemCases(ctx context.Context) error {
	seeds := []struct {
		fp, key, sev, dir, title, cause, action string
	}{
		{"cache_hit_ratio|warning|below", "cache_hit_ratio", "warning", "below", "缓存命中率偏低",
			"缓存命中率 {value} 偏低，可能存在全表扫描或 shared_buffers 不足", "检查热点 SQL 与 shared_buffers"},
		{"cache_hit_ratio|critical|below", "cache_hit_ratio", "critical", "below", "缓存命中率严重偏低",
			"缓存命中率 {value} 严重偏低", "立即排查全表扫描与缓冲配置"},
		{"conn_usage_ratio|warning|above", "conn_usage_ratio", "warning", "above", "连接使用率偏高",
			"连接使用率 {value} 偏高", "检查连接池与空闲连接"},
		{"conn_usage_ratio|critical|above", "conn_usage_ratio", "critical", "above", "连接接近打满",
			"连接使用率 {value} 接近上限", "扩容 max_connections 或限流应用连接"},
		{"max_xact_duration|warning|above", "max_xact_duration", "warning", "above", "存在长事务",
			"最长事务时长 {value}s", "定位并处理长事务会话"},
		{"max_xact_duration|critical|above", "max_xact_duration", "critical", "above", "存在超长事务",
			"最长事务时长 {value}s，可能阻塞复制/VACUUM", "尽快终止或提交异常长事务"},
		{"tps|warning|drop", "tps", "warning", "drop", "TPS 环比下跌",
			"TPS 相对基线下跌", "确认业务流量与主库可用性"},
		{"deadlocks_ps|warning|above", "deadlocks_ps", "warning", "above", "死锁增多",
			"死锁速率 {value}", "排查交叉更新与锁顺序"},
	}
	now := time.Now().UTC()
	for _, seed := range seeds {
		existing, _ := s.Store.GetSqlInsightCaseByFingerprint(ctx, seed.fp)
		if existing != nil {
			continue
		}
		_ = s.Store.UpsertSqlInsightCase(ctx, &store.SqlInsightCaseRow{
			ID: "seed-" + seed.fp, Fingerprint: seed.fp, MetricKey: seed.key, Severity: seed.sev, Direction: seed.dir,
			Title: seed.title, RootCauseTemplate: seed.cause, ActionTemplate: seed.action,
			HitCount: 0, System: true, LastSeenAt: &now,
		})
	}
	return nil
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
