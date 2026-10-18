package store

import "testing"

func TestBuildAlertHistoryRows(t *testing.T) {
	alerts := map[string]Alert{
		"a1": {ID: "a1", Title: "磁盘", Severity: "P2", Status: AlertStatusResolved, Handle: AlertHandleResolved},
		"a2": {ID: "a2", Title: "CPU", Severity: "P1", Status: AlertStatusFiring, Handle: AlertHandleUnclaimed},
	}
	events := []AlertEvent{
		{ID: "e1", AlertID: "a1", Action: "trigger", CreatedAt: "2026-10-18T01:00:00Z"},
		{ID: "e2", AlertID: "a1", Action: "notify", Detail: "通知外发：trigger", CreatedAt: "2026-10-18T01:00:01Z"},
		{ID: "e3", AlertID: "a1", Action: "notify", Detail: "通知外发：repeat", CreatedAt: "2026-10-18T01:05:00Z"},
		{ID: "e4", AlertID: "a1", Action: "resolve", Detail: "指标回落，告警自动恢复", Operator: "系统", CreatedAt: "2026-10-18T01:10:00Z"},
		{ID: "e5", AlertID: "a1", Action: "notify", Detail: "通知外发：resolve（告警已恢复）", CreatedAt: "2026-10-18T01:10:01Z"},
		{ID: "e6", AlertID: "a2", Action: "notify", Detail: "通知外发：trigger", CreatedAt: "2026-10-18T01:02:00Z"},
	}
	rows := BuildAlertHistoryRows(alerts, events, AlertFilter{})
	if len(rows) != 4 {
		t.Fatalf("want 4 rows got %d: %+v", len(rows), rows)
	}
	if rows[0].EventID != "e4" || rows[0].Kind != AlertHistoryKindResolve || rows[0].ID != "a1" {
		t.Fatalf("newest should be resolve of a1: %+v", rows[0])
	}
	if rows[1].EventID != "e3" || rows[1].NotifySeq != 2 || rows[3].EventID != "e2" || rows[3].NotifySeq != 1 {
		t.Fatalf("notify seq wrong: %+v", rows)
	}

	p1 := BuildAlertHistoryRows(alerts, events, AlertFilter{Severity: "P1"})
	if len(p1) != 1 || p1[0].ID != "a2" {
		t.Fatalf("severity filter: %+v", p1)
	}
	since := BuildAlertHistoryRows(alerts, events, AlertFilter{Since: "2026-10-18T01:04:00Z"})
	if len(since) != 2 || since[1].NotifySeq != 2 {
		t.Fatalf("since filter should keep seq from full history: %+v", since)
	}
}
