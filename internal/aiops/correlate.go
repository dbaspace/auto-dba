package aiops

import (
	"context"
	"sort"
	"strings"
	"time"

	"auto-dba/internal/store"
)

// Incident 伪事故：同实例（或 host）时间窗内 firing 告警聚合。
type Incident struct {
	Key             string           `json:"key"`
	InstanceID      string           `json:"instance_id,omitempty"`
	Host            string           `json:"host,omitempty"`
	Name            string           `json:"name,omitempty"`
	AlertCount      int              `json:"alert_count"`
	MaxSeverity     string           `json:"max_severity"`
	StartedAt       string           `json:"started_at,omitempty"`
	Alerts          []IncidentAlert  `json:"alerts"`
	RelatedTodos    []IncidentTodo   `json:"related_todos,omitempty"`
	RelatedDrifts   []DriftBrief     `json:"related_drifts,omitempty"`
	Summary         string           `json:"summary"`
}

type IncidentAlert struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	AlertName   string `json:"alertname,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
}

type IncidentTodo struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

var sevRank = map[string]int{"P0": 0, "P1": 1, "P2": 2, "P3": 3}

// CorrelateIncidents 聚合活跃告警。
func (s *Service) CorrelateIncidents(ctx context.Context, tenantID string, window time.Duration) ([]Incident, error) {
	if s == nil || s.Store == nil {
		return nil, nil
	}
	if window <= 0 {
		window = 30 * time.Minute
	}
	cutoff := time.Now().UTC().Add(-window)
	alerts, err := s.Store.ListAlerts(store.AlertFilter{Tab: "active", TenantID: tenantID})
	if err != nil {
		return nil, err
	}
	type bucket struct {
		key  string
		inst *ResolvedTarget
		list []store.Alert
	}
	groups := map[string]*bucket{}
	for _, a := range alerts {
		if a.Status == store.AlertStatusResolved {
			continue
		}
		if t, err := time.Parse(time.RFC3339, a.StartedAt); err == nil && t.Before(cutoff) {
			// 仍纳入：窗口用于「近期变更」对齐，活跃告警全量按实例分组
			_ = t
		}
		tgt := ResolveTarget(s.Store, a.Labels)
		key := "unknown"
		if tgt != nil && tgt.InstanceID != "" {
			key = "inst:" + tgt.InstanceID
		} else if tgt != nil && tgt.Host != "" {
			key = "host:" + tgt.Host
		} else if an := AlertNameFromLabels(a.Labels); an != "" {
			key = "name:" + an
		} else if a.Service != "" {
			key = "svc:" + a.Service
		}
		b := groups[key]
		if b == nil {
			b = &bucket{key: key, inst: tgt}
			groups[key] = b
		}
		b.list = append(b.list, a)
	}
	var out []Incident
	for _, b := range groups {
		if len(b.list) == 0 {
			continue
		}
		inc := Incident{Key: b.key, AlertCount: len(b.list)}
		if b.inst != nil {
			inc.InstanceID, inc.Host, inc.Name = b.inst.InstanceID, b.inst.Host, b.inst.Name
		}
		maxSev := "P3"
		var earliest string
		for _, a := range b.list {
			inc.Alerts = append(inc.Alerts, IncidentAlert{
				ID: a.ID, Fingerprint: a.Fingerprint, Severity: a.Severity, Title: a.Title,
				AlertName: AlertNameFromLabels(a.Labels), StartedAt: a.StartedAt,
			})
			if sevRank[a.Severity] < sevRank[maxSev] {
				maxSev = a.Severity
			}
			if earliest == "" || a.StartedAt < earliest {
				earliest = a.StartedAt
			}
			if todos, _ := s.Store.ListTodosBySource(store.TodoSourceAlert, a.ID, 3); len(todos) > 0 {
				for _, t := range todos {
					inc.RelatedTodos = append(inc.RelatedTodos, IncidentTodo{ID: t.ID, Title: t.Title, Status: t.Status})
				}
			}
		}
		inc.MaxSeverity = maxSev
		inc.StartedAt = earliest
		if b.inst != nil && b.inst.InstanceID != "" {
			drifts, _ := s.Store.ListCmdbDrifts(tenantID, 30)
			for _, d := range drifts {
				if d.CIType == "instance" && d.CIID == b.inst.InstanceID {
					inc.RelatedDrifts = append(inc.RelatedDrifts, DriftBrief{
						CIType: d.CIType, CIID: d.CIID, Name: d.Name, Field: d.Field,
					})
					if len(inc.RelatedDrifts) >= 5 {
						break
					}
				}
			}
		}
		inc.Summary = maxSev + " · 告警 " + itoa(len(b.list))
		if inc.Name != "" {
			inc.Summary += " · " + inc.Name
		} else if inc.InstanceID != "" {
			inc.Summary += " · " + inc.InstanceID
		}
		out = append(out, inc)
	}
	sort.Slice(out, func(i, j int) bool {
		if sevRank[out[i].MaxSeverity] != sevRank[out[j].MaxSeverity] {
			return sevRank[out[i].MaxSeverity] < sevRank[out[j].MaxSeverity]
		}
		return out[i].AlertCount > out[j].AlertCount
	})
	_ = ctx
	return out, nil
}

// GroupKeyForAlert 供测试。
func GroupKeyForAlert(db *store.DB, a *store.Alert) string {
	if a == nil {
		return ""
	}
	tgt := ResolveTarget(db, a.Labels)
	if tgt != nil && tgt.InstanceID != "" {
		return "inst:" + tgt.InstanceID
	}
	if tgt != nil && tgt.Host != "" {
		return "host:" + tgt.Host
	}
	if an := AlertNameFromLabels(a.Labels); an != "" {
		return "name:" + an
	}
	return "svc:" + strings.TrimSpace(a.Service)
}
