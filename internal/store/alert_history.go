package store

import (
	"sort"
	"strings"
)

const (
	AlertHistoryKindNotify  = "notify"
	AlertHistoryKindResolve = "resolve"
)

// AlertHistoryRow 告警历史一行：每次 trigger/repeat 外发一行，结案再一行；id 为父告警 id，用于对应。
type AlertHistoryRow struct {
	Alert
	EventID     string `json:"event_id"`
	Kind        string `json:"kind"` // notify | resolve
	EventAt     string `json:"event_at"`
	EventDetail string `json:"event_detail"`
	Operator    string `json:"operator"`
	NotifySeq   int    `json:"notify_seq,omitempty"`
}

func alertHistoryKind(e AlertEvent) string {
	switch e.Action {
	case "resolve":
		return AlertHistoryKindResolve
	case "notify":
		if strings.Contains(strings.ToLower(e.Detail), "resolve") {
			return ""
		}
		return AlertHistoryKindNotify
	}
	return ""
}

// BuildAlertHistoryRows 由事件与父告警生成历史行（按事件时间倒序）；过滤按父告警，since 按事件时间。
func BuildAlertHistoryRows(alerts map[string]Alert, events []AlertEvent, f AlertFilter) []AlertHistoryRow {
	sorted := append([]AlertEvent(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt < sorted[j].CreatedAt })
	kw := strings.ToLower(strings.TrimSpace(f.Keyword))
	match := f
	match.Tab = "all"
	match.Since = ""
	seq := map[string]int{}
	out := make([]AlertHistoryRow, 0, len(sorted))
	for _, e := range sorted {
		kind := alertHistoryKind(e)
		if kind == "" {
			continue
		}
		a, ok := alerts[e.AlertID]
		if !ok {
			continue
		}
		row := AlertHistoryRow{
			Alert: a, EventID: e.ID, Kind: kind, EventAt: e.CreatedAt, EventDetail: e.Detail, Operator: e.Operator,
		}
		if kind == AlertHistoryKindNotify {
			seq[e.AlertID]++
			row.NotifySeq = seq[e.AlertID]
		}
		if f.Since != "" && e.CreatedAt < f.Since {
			continue
		}
		if !alertInFilter(a, match, kw) {
			continue
		}
		out = append(out, row)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// ListAlertHistoryRows 告警历史：外发通知按次一行，结案一行。
func (d *DB) ListAlertHistoryRows(f AlertFilter) ([]AlertHistoryRow, error) {
	cols := strings.Split(alertCols, ", ")
	for i := range cols {
		cols[i] = "a." + cols[i]
	}
	q := `SELECT ` + strings.Join(cols, ", ") + `, e.id, e.action, e.detail, e.operator, e.created_at
		FROM tbl_alert_events e JOIN tbl_alerts a ON a.id = e.alert_id
		WHERE e.action IN ('notify', 'resolve')`
	args := []any{}
	if tid := strings.TrimSpace(f.TenantID); tid != "" {
		q += ` AND COALESCE(a.tenant_id,'')=?`
		args = append(args, tid)
	}
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	alerts := map[string]Alert{}
	var events []AlertEvent
	for rows.Next() {
		var e AlertEvent
		a, err := scanAlert(scanWithTail{rows: rows, tail: []any{&e.ID, &e.Action, &e.Detail, &e.Operator, &e.CreatedAt}})
		if err != nil {
			return nil, err
		}
		e.AlertID = a.ID
		alerts[a.ID] = *a
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return BuildAlertHistoryRows(alerts, events, f), nil
}

type scanWithTail struct {
	rows interface{ Scan(dest ...any) error }
	tail []any
}

func (s scanWithTail) Scan(dest ...any) error {
	return s.rows.Scan(append(dest, s.tail...)...)
}
