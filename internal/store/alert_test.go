package store

import (
	"testing"
	"time"
)

func TestAlertFingerprintStable(t *testing.T) {
	a := AlertFingerprint("rule-1", map[string]string{"b": "2", "a": "1", "__name__": "up"})
	b := AlertFingerprint("rule-1", map[string]string{"a": "1", "b": "2"})
	if a != b || a == "" {
		t.Fatalf("fingerprint unstable: %s vs %s", a, b)
	}
	c := AlertFingerprint("rule-2", map[string]string{"a": "1", "b": "2"})
	if a == c {
		t.Fatal("different rule should differ")
	}
}

func TestMatchersMatchLabels(t *testing.T) {
	m := ParseAlertMatchers("service=order, env=prod")
	if len(m) != 2 || m["service"] != "order" || m["env"] != "prod" {
		t.Fatalf("%+v", m)
	}
	if !MatchersMatchLabels(m, map[string]string{"service": "order", "env": "prod", "pod": "x"}) {
		t.Fatal("should match")
	}
	if MatchersMatchLabels(m, map[string]string{"service": "order", "env": "staging"}) {
		t.Fatal("should not match")
	}
	if MatchersMatchLabels(nil, map[string]string{"a": "1"}) {
		t.Fatal("empty matchers should not match")
	}
}

func TestParseAlertDuration(t *testing.T) {
	d, err := ParseAlertDuration("5m")
	if err != nil || d != 5*time.Minute {
		t.Fatalf("%v %v", d, err)
	}
	if _, err := ParseAlertDuration("abc"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRefreshSilenceStatus(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	s := &AlertSilence{
		StartsAt: now.Add(-time.Hour).Format(time.RFC3339),
		EndsAt:   now.Add(time.Hour).Format(time.RFC3339),
	}
	RefreshSilenceStatus(s, now)
	if s.Status != AlertSilenceActive {
		t.Fatalf("want active got %s", s.Status)
	}
	s.EndsAt = now.Add(-time.Minute).Format(time.RFC3339)
	RefreshSilenceStatus(s, now)
	if s.Status != AlertSilenceExpired {
		t.Fatalf("want expired got %s", s.Status)
	}
	s.StartsAt = now.Add(time.Hour).Format(time.RFC3339)
	s.EndsAt = now.Add(2 * time.Hour).Format(time.RFC3339)
	RefreshSilenceStatus(s, now)
	if s.Status != AlertSilencePending {
		t.Fatalf("want pending got %s", s.Status)
	}
}

func TestAlertInFilter(t *testing.T) {
	a := Alert{Status: AlertStatusFiring, Handle: AlertHandleUnclaimed, Severity: AlertSeverityP0, Title: "磁盘满", Service: "infra"}
	if !alertInFilter(a, AlertFilter{Tab: "active"}, "") {
		t.Fatal("active")
	}
	if !alertInFilter(a, AlertFilter{Tab: "unclaimed"}, "") {
		t.Fatal("unclaimed")
	}
	if alertInFilter(a, AlertFilter{Tab: "history"}, "") {
		t.Fatal("history should exclude firing")
	}
	a.Status = AlertStatusResolved
	a.Handle = AlertHandleResolved
	if !alertInFilter(a, AlertFilter{Tab: "history"}, "") {
		t.Fatal("history")
	}
	if !alertInFilter(Alert{Status: AlertStatusFiring, Title: "MySQL", Service: "db"}, AlertFilter{Tab: "active", Keyword: "mysql"}, "mysql") {
		t.Fatal("keyword")
	}
}

func TestNormalizeAlertRule(t *testing.T) {
	r := &AlertRule{Name: " x ", Expr: " up > 0 ", Severity: "P0", ForDuration: "1m"}
	if err := NormalizeAlertRule(r); err != nil {
		t.Fatal(err)
	}
	if r.Name != "x" || r.ForDuration != "1m" {
		t.Fatalf("%+v", r)
	}
	if err := NormalizeAlertRule(&AlertRule{Name: "a"}); err == nil {
		t.Fatal("expr required")
	}
}
