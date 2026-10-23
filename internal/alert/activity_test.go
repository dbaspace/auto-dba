package alert

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseWindowDefaultAndCap(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	since, until := ParseWindow("", "", 0, now)
	if !until.Equal(now) || now.Sub(since) != 24*time.Hour {
		t.Fatalf("default window %s .. %s", since, until)
	}
	since, until = ParseWindow("1600000000", "", 0, now)
	if now.Sub(since) != maxWindow {
		t.Fatalf("cap got %s", now.Sub(since))
	}
	before := now.Add(-time.Hour).Unix()
	_, until = ParseWindow("", "", before, now)
	if until.Unix() != before {
		t.Fatalf("before %d", until.Unix())
	}
}

func TestMergeActivityOrderAndCursor(t *testing.T) {
	a := []ActivityEvent{{ID: "a", Time: 30}, {ID: "b", Time: 10}}
	b := []ActivityEvent{{ID: "c", Time: 20}, {ID: "d", Time: 20}}
	page, next := MergeActivity([][]ActivityEvent{a, b}, 2)
	if len(page) != 2 || page[0].ID != "a" || page[1].ID != "c" || next != 20 {
		t.Fatalf("page %+v next %d", page, next)
	}
	page, next = MergeActivity([][]ActivityEvent{a}, 10)
	if len(page) != 2 || next != 0 {
		t.Fatalf("short page next %d", next)
	}
	page, _ = MergeActivity(nil, 5)
	if page == nil || len(page) != 0 {
		t.Fatalf("nil page")
	}
}

func TestMapAlertAndTicket(t *testing.T) {
	ev, ok := MapAlert(AlertInput{
		EventID: "e1", Action: "trigger", EventAt: "2026-10-22T10:00:00Z",
		AlertID: "a1", Severity: "P1", Title: "CPU 过高", Handle: "acknowledged", Owner: "zhang",
		CurValue: "81%", Threshold: "80%",
	})
	if !ok || ev.Kind != KindAlert || ev.Badge != "已认领 · zhang" || ev.Metric != "阈值 80%" {
		t.Fatalf("%+v ok %v", ev, ok)
	}
	if ev.Links[0].Label != "处理" || ev.Links[0].Path != "/ops/alerts?id=a1" {
		t.Fatalf("links %+v", ev.Links)
	}
	ev, ok = MapAlert(AlertInput{EventID: "e2", Action: "resolve", EventAt: "2026-10-22T11:00:00Z", AlertID: "a1", Title: "CPU"})
	if !ok || ev.Kind != KindRecover || ev.Links[0].Path != "/ops/alerts/history?alert_id=a1" {
		t.Fatalf("resolve %+v", ev)
	}
	_, ok = MapAlert(AlertInput{Action: "claim", EventAt: "2026-10-22T11:00:00Z"})
	if ok {
		t.Fatal("claim should not be a card")
	}
	ev, ok = MapTicket(TicketInput{ID: "i1", TicketNo: "T1", IssueType: "ddl", Description: "加索引", Status: "executed", UpdatedAt: "2026-10-22T09:00:00Z"})
	if !ok || ev.Kind != KindChange || ev.Title != "DDL · 加索引" {
		t.Fatalf("%+v", ev)
	}
	_, ok = MapTicket(TicketInput{ID: "i2", Status: "pending", UpdatedAt: "2026-10-22T09:00:00Z"})
	if ok {
		t.Fatal("pending ticket")
	}
}

func TestMapHAConfigAWR(t *testing.T) {
	ev, ok := MapHA(HAInput{ID: "h1", Action: "switchover", Status: "failed", UpdatedAt: "2026-10-22T08:00:00Z", ClusterName: "pg-a"})
	if !ok || ev.Title != "高可用 主从切换 · pg-a" || ev.Detail != "失败" {
		t.Fatalf("%+v", ev)
	}
	_, ok = MapHA(HAInput{ID: "h2", Status: "running", UpdatedAt: "2026-10-22T08:00:00Z"})
	if ok {
		t.Fatal("running ha")
	}
	ev, ok = MapConfig(ConfigInput{ID: "c1", Action: "update", CreatedAt: "2026-10-22T07:00:00Z", Source: ""})
	if !ok || ev.Title != "实例配置 修改" || ev.Detail != "来源 api" {
		t.Fatalf("%+v", ev)
	}
	ev, ok = MapAWR(AWRInput{Date: "2026-10-21", Total: 10, Anomaly: 2, At: 100})
	if !ok || ev.Kind != KindInspect || ev.Title != "AWR 日报完成 10 个实例" || ev.Detail != "2 个存在告警项，已生成巡检报告" {
		t.Fatalf("%+v", ev)
	}
	_, ok = MapAWR(AWRInput{Total: 0, At: 1})
	if ok {
		t.Fatal("empty awr")
	}
}

func TestLabelAndDNSMatch(t *testing.T) {
	if LabelInstanceID(map[string]string{"instanceId": "i-1"}) != "i-1" {
		t.Fatal("instanceId")
	}
	if LabelInstanceID(map[string]string{"ci_type": "host", "ci_id": "h"}) != "" {
		t.Fatal("host ci")
	}
	if LabelInstanceID(map[string]string{"ci_type": "instance", "ci_id": "i-2"}) != "i-2" {
		t.Fatal("ci instance")
	}
	refs := []InstRef{
		{ID: "a", Host: "10.20.5.31", Domain: "pg.example.com", Name: "pg-report"},
		{ID: "b", Host: "10.1.1.1", Domains: []string{"redis.example.com"}},
	}
	if got := MatchInstance(refs, []string{"10.20.5.31"}); got == nil || got.ID != "a" {
		t.Fatalf("ip %+v", got)
	}
	if got := MatchInstance(refs, []string{"Redis.Example.com"}); got == nil || got.ID != "b" {
		t.Fatalf("domain %+v", got)
	}
	if MatchInstance(refs, []string{"10.9.9.9"}) != nil {
		t.Fatal("miss")
	}
	raw := json.RawMessage(`{"name":"db","type":"A","data":"10.20.5.31"}`)
	got := MatchInstance(refs, SnapValues(raw))
	if got == nil || got.ID != "a" {
		t.Fatalf("snap %+v", got)
	}
	if len(SnapValues(nil)) != 0 {
		t.Fatal("empty snap")
	}
}

func TestWantKind(t *testing.T) {
	if !WantKind(nil, KindChange) {
		t.Fatal("empty means all")
	}
	kinds := ParseKinds("alert,nope,inspect")
	if len(kinds) != 2 || !WantKind(kinds, KindAlert) || WantKind(kinds, KindChange) {
		t.Fatalf("%v", kinds)
	}
}
