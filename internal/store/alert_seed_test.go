package store

import (
	"strings"
	"testing"
)

func TestDefaultAlertRulesValid(t *testing.T) {
	rules := DefaultAlertRules()
	if len(rules) != 6 {
		t.Fatalf("want 6 seed rules, got %d", len(rules))
	}
	for _, id := range obsoleteAlertSeedIDs {
		for _, r := range rules {
			if r.ID == id {
				t.Fatalf("obsolete seed still present: %s", id)
			}
		}
	}
	seen := map[string]struct{}{}
	for _, r := range rules {
		if r.ID == "" || !strings.HasPrefix(r.ID, "rule-seed-") {
			t.Fatalf("bad seed id: %q", r.ID)
		}
		if _, ok := seen[r.ID]; ok {
			t.Fatalf("duplicate seed id: %s", r.ID)
		}
		seen[r.ID] = struct{}{}
		cp := r
		if err := NormalizeAlertRule(&cp); err != nil {
			t.Fatalf("%s: NormalizeAlertRule: %v", r.ID, err)
		}
		if _, err := ParseAlertDuration(cp.ForDuration); err != nil {
			t.Fatalf("%s: for %q: %v", r.ID, cp.ForDuration, err)
		}
		if !ValidAlertSeverity(cp.Severity) {
			t.Fatalf("%s: severity %q", r.ID, cp.Severity)
		}
		if !cp.Enabled {
			t.Fatalf("%s: should be enabled", r.ID)
		}
		if cp.CreatedBy != "system" {
			t.Fatalf("%s: created_by want system", r.ID)
		}
		if strings.Contains(cp.Expr, "__SEL__") {
			t.Fatalf("%s: expr must not contain __SEL__", r.ID)
		}
		if !strings.Contains(cp.Expr, AlertScopeToken) {
			t.Fatalf("%s: expr must contain %s", r.ID, AlertScopeToken)
		}
		if cp.Scope.Mode != AlertScopeModeGlobal {
			t.Fatalf("%s: seed scope want global", r.ID)
		}
		if cp.Labels["team"] != "DBA" {
			t.Fatalf("%s: labels.team want DBA", r.ID)
		}
		if cp.Annotations["summary"] == "" {
			t.Fatalf("%s: missing annotations.summary", r.ID)
		}
		switch r.ID {
		case "rule-seed-host-disk", "rule-seed-mysql-conn", "rule-seed-pg-storage":
			if len(cp.Tiers) < 2 || cp.MetricExpr == "" {
				t.Fatalf("%s should be multi-tier: tiers=%d metric=%q", r.ID, len(cp.Tiers), cp.MetricExpr)
			}
		}
	}
}
