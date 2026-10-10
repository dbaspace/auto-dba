package store

import (
	"testing"
	"time"
)

func TestPickAlertTierHighest(t *testing.T) {
	tiers := []AlertTier{
		{Op: ">=", Value: 0.8, Severity: AlertSeverityP1, For: "5m"},
		{Op: ">=", Value: 0.9, Severity: AlertSeverityP0, For: "3m"},
	}
	if PickAlertTier(tiers, 0.75) != nil {
		t.Fatal("below all")
	}
	hit := PickAlertTier(tiers, 0.85)
	if hit == nil || hit.Severity != AlertSeverityP1 {
		t.Fatalf("%+v", hit)
	}
	hit2 := PickAlertTier(tiers, 0.95)
	if hit2 == nil || hit2.Severity != AlertSeverityP0 {
		t.Fatalf("%+v", hit2)
	}
}

func TestDecideAlertNotifyRateAndRepeat(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	pol := AlertNotifyPolicy{MaxFirePerWindow: 2, Window: "1h", RepeatInterval: "30m", RepeatOnHandles: []string{AlertHandleUnclaimed}}
	a := &Alert{Status: AlertStatusFiring, Handle: AlertHandleUnclaimed}

	d := DecideAlertNotify(a, pol, now, true)
	if !d.Should || d.Reason != "trigger" {
		t.Fatalf("%+v", d)
	}
	ApplyAlertNotifyMark(a, pol, now)
	if a.NotifyCount != 1 {
		t.Fatal(a.NotifyCount)
	}

	d2 := DecideAlertNotify(a, pol, now.Add(5*time.Minute), false)
	if d2.Should || d2.Skip != "not_due" {
		t.Fatalf("%+v", d2)
	}

	d3 := DecideAlertNotify(a, pol, now.Add(31*time.Minute), false)
	if !d3.Should || d3.Reason != "repeat" {
		t.Fatalf("%+v", d3)
	}
	ApplyAlertNotifyMark(a, pol, now.Add(31*time.Minute))

	d4 := DecideAlertNotify(a, pol, now.Add(62*time.Minute), false)
	if d4.Should || d4.Skip != "rate_limited" {
		t.Fatalf("want rate_limited got %+v count=%d", d4, a.NotifyCount)
	}

	a.Handle = AlertHandleAcknowledged
	a.NotifyCount = 0
	a.LastNotifiedAt = now.Format(time.RFC3339)
	d5 := DecideAlertNotify(a, pol, now.Add(40*time.Minute), false)
	if d5.Should || d5.Skip != "handle" {
		t.Fatalf("%+v", d5)
	}
}
