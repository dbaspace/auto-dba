package aiops

import "auto-dba/internal/store"

func knowledgeCaseRows() []*store.SqlInsightCaseRow {
	list := AllKnowledgeCases()
	out := make([]*store.SqlInsightCaseRow, 0, len(list))
	for _, kc := range list {
		out = append(out, &store.SqlInsightCaseRow{
			Fingerprint: kc.Fingerprint, MetricKey: kc.MetricKey, Severity: kc.Severity,
			Direction: kc.Direction, Title: kc.Title,
			RootCauseTemplate: kc.RootCauseTemplate, ActionTemplate: kc.ActionTemplate,
			AlertNames: JoinAlertNames(kc.AlertNames), System: true,
		})
	}
	return out
}

// mergeCaseRows 以 fingerprint 去重，DB 行优先。
func mergeCaseRows(dbRows, kbRows []*store.SqlInsightCaseRow) []*store.SqlInsightCaseRow {
	seen := map[string]struct{}{}
	var out []*store.SqlInsightCaseRow
	for _, c := range dbRows {
		if c == nil || c.Fingerprint == "" {
			continue
		}
		if _, ok := seen[c.Fingerprint]; ok {
			continue
		}
		seen[c.Fingerprint] = struct{}{}
		out = append(out, c)
	}
	for _, c := range kbRows {
		if c == nil || c.Fingerprint == "" {
			continue
		}
		if _, ok := seen[c.Fingerprint]; ok {
			continue
		}
		seen[c.Fingerprint] = struct{}{}
		out = append(out, c)
	}
	return out
}
