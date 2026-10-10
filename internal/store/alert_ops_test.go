package store

import (
	"strings"
	"testing"
	"time"
)

func TestAlertInhibitedBy(t *testing.T) {
	src := Alert{
		ID: "a1", Status: AlertStatusFiring,
		Labels: map[string]string{"alertname": "HostDown", "instance": "db1"},
	}
	tgt := Alert{
		ID: "a2", Status: AlertStatusFiring,
		Labels: map[string]string{"alertname": "DiskHigh", "instance": "db1"},
	}
	rules := []AlertInhibition{{
		Enabled: true,
		SourceMatchers: "alertname=HostDown",
		TargetMatchers: "alertname=DiskHigh",
		EqualLabels:    "instance",
	}}
	if !AlertInhibitedBy(&tgt, []Alert{src}, rules) {
		t.Fatal("should inhibit")
	}
	tgt.Labels["instance"] = "db2"
	if AlertInhibitedBy(&tgt, []Alert{src}, rules) {
		t.Fatal("equal label mismatch")
	}
}

func TestMatchAlertRoutesAndGroupKey(t *testing.T) {
	routes := []AlertRoute{
		{Enabled: true, Priority: 10, Matchers: "severity=P0", Channels: []string{"phone", "wecom"}, GroupBy: []string{"service"}, Continue: false},
		{Enabled: true, Priority: 100, Matchers: "", Channels: []string{"webhook"}, GroupBy: []string{"alertname"}},
	}
	hit := MatchAlertRoutes(map[string]string{"severity": "P0", "service": "mysql"}, routes)
	if len(hit) != 1 || hit[0].Channels[0] != "phone" {
		t.Fatalf("%+v", hit)
	}
	ch := MergeRouteChannels(hit)
	if strings.Join(ch, ",") != "phone,wecom" {
		t.Fatal(ch)
	}
	gk := AlertGroupKey(map[string]string{"service": "mysql", "instance": "a"}, []string{"service"})
	if gk != "service=mysql" {
		t.Fatal(gk)
	}
}

func TestOncallWindow(t *testing.T) {
	// 用内存 DB 太重；只测 parseHM + 逻辑片段
	m, err := parseHM("09:30")
	if err != nil || m != 9*60+30 {
		t.Fatal(m, err)
	}
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.Local) // Friday
	if int(now.Weekday()) != 5 {
		t.Fatalf("weekday %d", now.Weekday())
	}
}
