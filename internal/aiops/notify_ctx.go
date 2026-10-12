package aiops

import (
	"context"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// NotifyContext Webhook / IM 外发附加的轻量上下文（避免完整 Bundle 体积）。
type NotifyContext struct {
	InstanceID    string     `json:"instance_id,omitempty"`
	ImpactSummary string     `json:"impact_summary,omitempty"`
	CaseHints     []CaseHint `json:"case_hints,omitempty"`
	Summary       string     `json:"context_summary,omitempty"`
}

// BuildNotifyContext 为告警外发构建摘要（失败时返回 nil，不影响主路径）。
func (s *Service) BuildNotifyContext(a *store.Alert, rule *store.AlertRule) *NotifyContext {
	if s == nil || s.Store == nil || a == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	nc := &NotifyContext{}
	target := ResolveTarget(s.Store, a.Labels)
	if target != nil {
		nc.InstanceID = target.InstanceID
		if target.CIType != "" && target.CIID != "" {
			tid := strings.TrimSpace(a.TenantID)
			if rep, err := s.cmdbSvc().ImpactInTenant(target.CIType, target.CIID, tid, "down", 2); err == nil && rep != nil {
				nc.ImpactSummary = FormatImpactSummary(rep.Summary.Downstream, rep.Summary.Upstream, rep.Summary.AppTags)
			}
		}
	}
	cases, _, err := s.Store.ListSqlInsightCases(ctx, 0, 100)
	if err != nil {
		cases = nil
	}
	cases = mergeCaseRows(cases, knowledgeCaseRows())
	hints := MatchCasesForAlert(cases, a, rule)
	if len(hints) > 3 {
		hints = hints[:3]
	}
	nc.CaseHints = hints

	var parts []string
	if nc.ImpactSummary != "" {
		parts = append(parts, nc.ImpactSummary)
	}
	if len(hints) > 0 {
		parts = append(parts, "案例:"+hints[0].Title)
	}
	if target != nil && target.Name != "" {
		parts = append(parts, "实例:"+target.Name)
	}
	nc.Summary = strings.Join(parts, " · ")
	if nc.Summary == "" && nc.InstanceID == "" && len(nc.CaseHints) == 0 {
		return nil
	}
	return nc
}

// FormatImpactSummary 一行影响摘要。
func FormatImpactSummary(down, up int, appTags []string) string {
	s := "影响下游=" + itoa(down) + " 上游=" + itoa(up)
	if len(appTags) > 0 {
		s += " 应用=" + strings.Join(appTags, ",")
	}
	return s
}
