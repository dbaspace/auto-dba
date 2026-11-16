package sessionstore

import (
	"testing"
	"time"
)

func TestKeepOrFreshSameDayKeepsExpire(t *testing.T) {
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, shanghai)
	prev := now.Add(20 * time.Hour)
	got, day := KeepOrFresh(now, 24*time.Hour, prev, ShanghaiDay(now), now.Add(-time.Hour).UTC().Format(time.RFC3339))
	if !got.Equal(prev) || day != "2026-11-16" {
		t.Fatalf("got %s %s", got, day)
	}
}

func TestKeepOrFreshNextDayStartsWindow(t *testing.T) {
	now := time.Date(2026, 11, 17, 7, 0, 0, 0, shanghai)
	prev := now.Add(time.Hour)
	got, day := KeepOrFresh(now, 24*time.Hour, prev, "2026-11-16", "")
	if !got.Equal(now.Add(24*time.Hour)) || day != "2026-11-17" {
		t.Fatalf("got %s %s", got, day)
	}
}

func TestKeepOrFreshExpiredStartsWindow(t *testing.T) {
	now := time.Date(2026, 11, 16, 18, 0, 0, 0, shanghai)
	prev := now.Add(-time.Minute)
	got, day := KeepOrFresh(now, 24*time.Hour, prev, ShanghaiDay(now), "")
	if !got.Equal(now.Add(24*time.Hour)) || day != "2026-11-16" {
		t.Fatalf("got %s %s", got, day)
	}
}

func TestVisitExtendUsesOnlineAtWhenDateMissing(t *testing.T) {
	now := time.Date(2026, 11, 17, 7, 0, 0, 0, shanghai)
	expire := now.Add(2 * time.Hour)
	anchor := time.Date(2026, 11, 16, 8, 0, 0, 0, shanghai)
	if !VisitExtend(now, expire, "", anchor) {
		t.Fatal("yesterday anchor should extend")
	}
	if VisitExtend(now, expire, "", now) {
		t.Fatal("today anchor should not extend")
	}
	if VisitExtend(now, now.Add(-time.Minute), "2026-11-16", anchor) {
		t.Fatal("expired session must not extend")
	}
}
