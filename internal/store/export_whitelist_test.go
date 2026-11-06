package store

import (
	"testing"
	"time"
)

func TestExportWhitelistAllows(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour).Format(time.RFC3339)
	past := now.Add(-time.Hour).Format(time.RFC3339)
	if !exportWhitelistAllows([]string{"db-1"}, "", "db-1", now) {
		t.Fatal("empty expire should allow")
	}
	if !exportWhitelistAllows([]string{"db-1"}, later, "db-1", now) {
		t.Fatal("future expire should allow")
	}
	if exportWhitelistAllows([]string{"db-1"}, past, "db-1", now) {
		t.Fatal("expired")
	}
	if exportWhitelistAllows([]string{"db-2"}, "", "db-1", now) {
		t.Fatal("other db")
	}
	if exportWhitelistAllows([]string{"db-1"}, "not-a-time", "db-1", now) {
		t.Fatal("bad expire")
	}
}
