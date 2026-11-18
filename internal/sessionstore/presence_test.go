package sessionstore

import (
	"testing"
	"time"
)

func TestResolvePresence(t *testing.T) {
	now := time.Date(2026, 11, 18, 2, 0, 0, 0, time.UTC)
	recent := now.Add(-5 * time.Minute).Format(time.RFC3339)
	older := now.Add(-30 * time.Minute).Format(time.RFC3339)
	pageNewer := now.Add(-2 * time.Minute).Format(time.RFC3339)

	presence, at, source := ResolvePresence(recent, "", now)
	if presence != "active" || source != "page" || at == "" {
		t.Fatalf("page recent: %s %s %s", presence, at, source)
	}
	presence, _, source = ResolvePresence(older, "", now)
	if presence != "away" || source != "page" {
		t.Fatalf("page old: %s %s", presence, source)
	}
	presence, _, source = ResolvePresence("", "", now)
	if presence != "away" || source != "" {
		t.Fatalf("empty: %s %s", presence, source)
	}
	presence, _, source = ResolvePresence("", recent, now)
	if presence != "active" || source != "pat" {
		t.Fatalf("pat recent: %s %s", presence, source)
	}
	presence, _, source = ResolvePresence(older, recent, now)
	if presence != "active" || source != "pat" {
		t.Fatalf("pat newer: %s %s", presence, source)
	}
	presence, _, source = ResolvePresence(pageNewer, recent, now)
	if presence != "active" || source != "page" {
		t.Fatalf("page newer: %s %s", presence, source)
	}
}
