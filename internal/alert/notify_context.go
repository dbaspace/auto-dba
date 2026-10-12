package alert

import (
	"auto-dba/internal/aiops"
	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

func buildAlertNotifyContext(db *store.DB, a *store.Alert, rule *store.AlertRule) *notify.AlertContextOpts {
	if db == nil || a == nil {
		return nil
	}
	nc := aiops.New(db).BuildNotifyContext(a, rule)
	if nc == nil {
		return nil
	}
	out := &notify.AlertContextOpts{
		InstanceID:     nc.InstanceID,
		ImpactSummary:  nc.ImpactSummary,
		ContextSummary: nc.Summary,
	}
	for _, h := range nc.CaseHints {
		out.CaseHints = append(out.CaseHints, notify.AlertCaseHint{
			Fingerprint: h.Fingerprint,
			MetricKey:   h.MetricKey,
			Title:       h.Title,
			MatchReason: h.MatchReason,
		})
	}
	return out
}
