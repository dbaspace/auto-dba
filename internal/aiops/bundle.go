package aiops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto-dba/internal/cmdb"
	"auto-dba/internal/store"
)

// ContextBundle 告警诊断上下文包（Phase 0，无 LLM）。
type ContextBundle struct {
	AlertID     string                 `json:"alert_id"`
	Fingerprint string                 `json:"fingerprint"`
	AlertName   string                 `json:"alertname,omitempty"`
	GeneratedAt string                 `json:"generated_at"`
	Target      *ResolvedTarget        `json:"target,omitempty"`
	Rule        *RuleBrief             `json:"rule,omitempty"`
	Impact      *ImpactBrief           `json:"impact,omitempty"`
	Cases       []CaseHint             `json:"cases,omitempty"`
	Findings    []FindingBrief         `json:"findings,omitempty"`
	History     []HistoryBrief         `json:"history,omitempty"`
	Todos       []TodoBrief            `json:"todos,omitempty"`
	Drifts      []DriftBrief           `json:"drifts,omitempty"`
	Summary     string                 `json:"summary"`
	Sources     []string               `json:"sources,omitempty"`
}

type RuleBrief struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Severity string `json:"severity,omitempty"`
	Expr     string `json:"expr,omitempty"`
}

type ImpactBrief struct {
	Direction  string         `json:"direction"`
	Depth      int            `json:"depth"`
	Upstream   int            `json:"upstream"`
	Downstream int            `json:"downstream"`
	ByType     map[string]int `json:"by_type,omitempty"`
	AppTags    []string       `json:"app_tags,omitempty"`
	BizTags    []string       `json:"biz_tags,omitempty"`
}

type FindingBrief struct {
	ID          string `json:"id"`
	MetricKey   string `json:"metric_key"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Message     string `json:"message,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Suggestion  string `json:"suggestion,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type HistoryBrief struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at,omitempty"`
	ResolvedAt string `json:"resolved_at,omitempty"`
}

type TodoBrief struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Owner  string `json:"owner,omitempty"`
}

type DriftBrief struct {
	CIType string `json:"ci_type"`
	CIID   string `json:"ci_id"`
	Name   string `json:"name,omitempty"`
	Field  string `json:"field,omitempty"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// Service 组装 Context Bundle。
type Service struct {
	Store *store.DB
	Cmdb  *cmdb.Service
}

func New(db *store.DB) *Service {
	return &Service{Store: db, Cmdb: cmdb.New(db)}
}

func (s *Service) cmdbSvc() *cmdb.Service {
	if s.Cmdb != nil {
		return s.Cmdb
	}
	s.Cmdb = cmdb.New(s.Store)
	return s.Cmdb
}

// BuildAlertContextBundle 聚合告警相关只读上下文。
func (s *Service) BuildAlertContextBundle(ctx context.Context, alertID, tenantID string) (*ContextBundle, error) {
	if s == nil || s.Store == nil {
		return nil, nil
	}
	a, err := s.Store.GetAlertInTenant(alertID, tenantID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, nil
	}
	return s.build(ctx, a, tenantID)
}

func (s *Service) build(ctx context.Context, a *store.Alert, tenantID string) (*ContextBundle, error) {
	b := &ContextBundle{
		AlertID: a.ID, Fingerprint: a.Fingerprint,
		AlertName: AlertNameFromLabels(a.Labels),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	var sources []string

	var rule *store.AlertRule
	if a.RuleID != "" {
		if r, err := s.Store.GetAlertRule(a.RuleID); err == nil && r != nil {
			rule = r
			b.Rule = &RuleBrief{ID: r.ID, Name: r.Name, Severity: r.Severity, Expr: r.EvalExpr()}
			sources = append(sources, "alert_rule")
		}
	}

	b.Target = ResolveTarget(s.Store, a.Labels)
	if b.Target != nil && b.Target.CIType != "" && b.Target.CIID != "" {
		if rep, err := s.cmdbSvc().ImpactInTenant(b.Target.CIType, b.Target.CIID, tenantID, "down", 2); err == nil && rep != nil {
			b.Impact = &ImpactBrief{
				Direction: rep.Direction, Depth: rep.Depth,
				Upstream: rep.Summary.Upstream, Downstream: rep.Summary.Downstream,
				ByType: rep.Summary.ByType, AppTags: rep.Summary.AppTags, BizTags: rep.Summary.BizTags,
			}
			sources = append(sources, "cmdb_impact")
		}
	}

	cases, _, err := s.Store.ListSqlInsightCases(ctx, 0, 200)
	if err != nil {
		cases = nil
	}
	// 库内案例与内置知识库合并（内置补齐未 seed / 空库场景）
	cases = mergeCaseRows(cases, knowledgeCaseRows())
	b.Cases = MatchCasesForAlert(cases, a, rule)
	if len(b.Cases) > 0 {
		sources = append(sources, "sql_insight_cases")
	}

	if b.Target != nil {
		findings, _ := s.Store.ListRecentSqlInsightFindings(ctx, b.Target.InstanceID, b.Target.Host, b.Target.Port, 8)
		for _, f := range findings {
			if f == nil {
				continue
			}
			b.Findings = append(b.Findings, FindingBrief{
				ID: f.ID, MetricKey: f.MetricKey, Severity: f.Severity, Title: f.Title,
				Message: f.Message, Fingerprint: f.Fingerprint, Suggestion: f.Suggestion,
				CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
		if len(b.Findings) > 0 {
			sources = append(sources, "sql_insight_findings")
		}
	}

	hist, _ := s.Store.ListAlertsByFingerprint(a.Fingerprint, 8)
	for _, h := range hist {
		if h.ID == a.ID {
			continue
		}
		b.History = append(b.History, HistoryBrief{
			ID: h.ID, Severity: h.Severity, Title: h.Title, Status: h.Status,
			StartedAt: h.StartedAt, ResolvedAt: h.ResolvedAt,
		})
	}
	if len(b.History) > 0 {
		sources = append(sources, "alert_history")
	}

	todos, _ := s.Store.ListTodosBySource(store.TodoSourceAlert, a.ID, 5)
	for _, t := range todos {
		b.Todos = append(b.Todos, TodoBrief{ID: t.ID, Title: t.Title, Status: t.Status, Owner: t.Owner})
	}
	if len(b.Todos) > 0 {
		sources = append(sources, "todos")
	}

	if b.Target != nil && b.Target.InstanceID != "" {
		drifts, _ := s.Store.ListCmdbDrifts(tenantID, 50)
		for _, d := range drifts {
			if d.CIType == "instance" && d.CIID == b.Target.InstanceID {
				b.Drifts = append(b.Drifts, DriftBrief{
					CIType: d.CIType, CIID: d.CIID, Name: d.Name, Field: d.Field,
					Before: fmt.Sprint(d.Baseline), After: fmt.Sprint(d.Current),
				})
				if len(b.Drifts) >= 5 {
					break
				}
			}
		}
		if len(b.Drifts) > 0 {
			sources = append(sources, "cmdb_drift")
		}
	}

	b.Sources = sources
	b.Summary = buildSummary(a, b)
	return b, nil
}

func buildSummary(a *store.Alert, b *ContextBundle) string {
	var parts []string
	parts = append(parts, a.Severity+" "+a.Title)
	if b.Target != nil && b.Target.Name != "" {
		parts = append(parts, "实例="+b.Target.Name)
	} else if b.Target != nil && b.Target.InstanceID != "" {
		parts = append(parts, "instance_id="+b.Target.InstanceID)
	}
	if b.Impact != nil {
		parts = append(parts, "影响下游="+itoa(b.Impact.Downstream))
		if len(b.Impact.AppTags) > 0 {
			parts = append(parts, "应用标签="+strings.Join(b.Impact.AppTags, ","))
		}
	}
	if len(b.Cases) > 0 {
		parts = append(parts, "案例="+b.Cases[0].Title)
	}
	if n := len(b.History); n > 0 {
		parts = append(parts, "历史同类="+itoa(n))
	}
	if n := len(b.Findings); n > 0 {
		parts = append(parts, "近期洞察="+itoa(n))
	}
	if n := len(b.Drifts); n > 0 {
		parts = append(parts, "漂移="+itoa(n))
	}
	return strings.Join(parts, " · ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
