package store

import (
	"strings"
	"time"
)

const (
	AlertHistoryKindNotify  = "notify"
	AlertHistoryKindResolve = "resolve"

	alertHistoryDefaultPageSize = 20
	alertHistoryMaxPageSize     = 200
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

// AlertHistoryQuery 历史分页查询；AlertID 非空时只看该告警的通知/恢复行。
type AlertHistoryQuery struct {
	AlertFilter
	AlertID  string
	Page     int
	PageSize int
}

// normalizeAlertTime 统一为与存储一致的 UTC RFC3339，保证字符串比较正确。
func normalizeAlertTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return nowRFC3339(t)
	}
	return s
}

func (q AlertHistoryQuery) limitOffset() (int, int) {
	size := q.PageSize
	if size <= 0 {
		size = alertHistoryDefaultPageSize
	}
	if size > alertHistoryMaxPageSize {
		size = alertHistoryMaxPageSize
	}
	page := q.Page
	if page <= 0 {
		page = 1
	}
	return size, (page - 1) * size
}

// alertHistoryWhere 外层过滤：事件时间窗 + 父告警租户/级别/服务/关键字/告警 id。
func alertHistoryWhere(q AlertHistoryQuery) (string, []any) {
	var b strings.Builder
	var args []any
	b.WriteString(` WHERE 1=1`)
	if s := normalizeAlertTime(q.Since); s != "" {
		b.WriteString(` AND ev.created_at >= ?`)
		args = append(args, s)
	}
	if u := normalizeAlertTime(q.Until); u != "" {
		b.WriteString(` AND ev.created_at <= ?`)
		args = append(args, u)
	}
	if tid := strings.TrimSpace(q.TenantID); tid != "" {
		b.WriteString(` AND COALESCE(a.tenant_id,'')=?`)
		args = append(args, tid)
	}
	if sev := strings.TrimSpace(q.Severity); sev != "" {
		b.WriteString(` AND a.severity=?`)
		args = append(args, sev)
	}
	if svc := strings.TrimSpace(q.Service); svc != "" {
		b.WriteString(` AND a.service=?`)
		args = append(args, svc)
	}
	if id := strings.TrimSpace(q.AlertID); id != "" {
		b.WriteString(` AND a.id=?`)
		args = append(args, id)
	}
	if kw := strings.ToLower(strings.TrimSpace(q.Keyword)); kw != "" {
		b.WriteString(` AND LOWER(a.title || ' ' || a.service || ' ' || a.cluster || ' ' || a.id || ' ' || a.owner) LIKE ?`)
		args = append(args, "%"+kw+"%")
	}
	return b.String(), args
}

// alertHistoryFrom 事件 CTE：恢复外发通知不单列；notify_seq 按该告警全生命周期累计（不受时间窗影响）。
func alertHistoryFrom(q AlertHistoryQuery) (string, []any) {
	var winArgs []any
	win := ""
	if s := normalizeAlertTime(q.Since); s != "" {
		win += ` AND w.created_at >= ?`
		winArgs = append(winArgs, s)
	}
	if u := normalizeAlertTime(q.Until); u != "" {
		win += ` AND w.created_at <= ?`
		winArgs = append(winArgs, u)
	}
	scope := ""
	if win != "" {
		scope = ` AND e.alert_id IN (SELECT w.alert_id FROM tbl_alert_events w WHERE w.action IN ('notify', 'resolve')` + win + `)`
	}
	where, args := alertHistoryWhere(q)
	from := ` FROM (
		SELECT e.id, e.alert_id, e.action, e.detail, e.operator, e.created_at,
			CASE WHEN e.action = 'notify' THEN ROW_NUMBER() OVER (PARTITION BY e.alert_id, e.action ORDER BY e.created_at, e.id) ELSE 0 END AS notify_seq
		FROM tbl_alert_events e
		WHERE (e.action = 'resolve' OR (e.action = 'notify' AND LOWER(e.detail) NOT LIKE '%resolve%'))` + scope + `
	) ev JOIN tbl_alerts a ON a.id = ev.alert_id` + where
	return from, append(winArgs, args...)
}

// ListAlertHistoryRows 告警历史分页：外发通知按次一行，结案一行，按事件时间倒序。
func (d *DB) ListAlertHistoryRows(q AlertHistoryQuery) ([]AlertHistoryRow, int, error) {
	from, args := alertHistoryFrom(q)
	var total int
	if err := d.QueryRow(`SELECT COUNT(*)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	out := []AlertHistoryRow{}
	if total == 0 {
		return out, 0, nil
	}
	cols := strings.Split(alertCols, ", ")
	for i, c := range cols {
		if c == "annotations" {
			cols[i] = "'' AS annotations"
			continue
		}
		cols[i] = "a." + c
	}
	limit, offset := q.limitOffset()
	rows, err := d.Query(`SELECT `+strings.Join(cols, ", ")+`, ev.id, ev.action, ev.detail, ev.operator, ev.created_at, ev.notify_seq`+
		from+` ORDER BY ev.created_at DESC, ev.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var r AlertHistoryRow
		a, err := scanAlert(scanWithTail{rows: rows, tail: []any{&r.EventID, &r.Kind, &r.EventDetail, &r.Operator, &r.EventAt, &r.NotifySeq}})
		if err != nil {
			return nil, 0, err
		}
		r.Alert = *a
		out = append(out, r)
	}
	return out, total, rows.Err()
}

type scanWithTail struct {
	rows interface{ Scan(dest ...any) error }
	tail []any
}

func (s scanWithTail) Scan(dest ...any) error {
	return s.rows.Scan(append(dest, s.tail...)...)
}
