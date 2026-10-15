package store

import (
	"testing"
	"time"
)

func TestResolveAlertNotifyPolicyInherit(t *testing.T) {
	SyncGlobalAlertNotifyPolicy(2, "15m", "5m")
	p := ResolveAlertNotifyPolicy(AlertNotifyPolicy{InheritGlobal: true})
	if p.MaxFirePerWindow != 2 || p.Window != "15m" || p.RepeatInterval != "5m" {
		t.Fatalf("%+v", p)
	}
	explicit := ResolveAlertNotifyPolicy(AlertNotifyPolicy{MaxFirePerWindow: 1, Window: "10m", RepeatInterval: "2m"})
	if explicit.MaxFirePerWindow != 1 || explicit.Window != "10m" {
		t.Fatalf("%+v", explicit)
	}
}

func TestLegacyDefaultBecomesInherit(t *testing.T) {
	p := decodeAlertNotifyPolicy(`{"max_fire_per_window":3,"window":"1h","repeat_interval":"30m","repeat_on_handles":["unclaimed"]}`)
	if !p.InheritGlobal {
		t.Fatalf("want inherit got %+v", p)
	}
}

func TestDecideWithFingerprintBudgetWindow(t *testing.T) {
	SyncGlobalAlertNotifyPolicy(3, "15m", "5m")
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	pol := AlertNotifyPolicy{MaxFirePerWindow: 3, Window: "15m", RepeatInterval: "5m", RepeatOnHandles: []string{AlertHandleUnclaimed}}
	a := &Alert{Status: AlertStatusFiring, Handle: AlertHandleUnclaimed, Fingerprint: "fp1"}

	for i := 0; i < 3; i++ {
		d := DecideAlertNotify(a, pol, now.Add(time.Duration(i)*5*time.Minute), i == 0)
		if !d.Should {
			t.Fatalf("send #%d: %+v", i+1, d)
		}
		ApplyAlertNotifyMark(a, pol, now.Add(time.Duration(i)*5*time.Minute))
	}
	d4 := DecideAlertNotify(a, pol, now.Add(12*time.Minute), false)
	if d4.Should || d4.Skip != "rate_limited" {
		t.Fatalf("want rate_limited got %+v", d4)
	}
	// 窗口过期后可再发
	d5 := DecideAlertNotify(a, pol, now.Add(12*time.Minute).Add(15*time.Minute), true)
	if !d5.Should {
		t.Fatalf("after window %+v", d5)
	}
}
