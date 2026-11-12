package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestPickHostKeeper(t *testing.T) {
	g := []store.Instance{
		{ID: "a", CreatedAt: "2026-01-01"},
		{ID: "b", CreatedAt: "2026-02-01", CloudInstanceID: "ins-1"},
		{ID: "c", CreatedAt: "2025-12-01", EnvID: "e1", RegionID: "r1"},
	}
	if got := g[pickHostKeeper(g)].ID; got != "b" {
		t.Fatalf("keeper=%s want b", got)
	}
	g = []store.Instance{{ID: "a", CreatedAt: "2026-02-01"}, {ID: "b", CreatedAt: "2026-01-01"}}
	if got := g[pickHostKeeper(g)].ID; got != "b" {
		t.Fatalf("keeper=%s want b (earlier)", got)
	}
}

func TestFillHostBlanks(t *testing.T) {
	dst := store.Instance{CloudInstanceID: "ins-1", Host: "10.0.0.1"}
	src := store.Instance{CloudInstanceID: "ins-2", EnvID: "e1", IDC: "singapore", CloudRegion: "ap-singapore", Tags: []string{"a"}, Host: "10.0.0.1:22"}
	if !fillHostBlanks(&dst, &src) {
		t.Fatal("expected change")
	}
	if dst.CloudInstanceID != "ins-1" || dst.EnvID != "e1" || dst.IDC != "singapore" || dst.CloudRegion != "ap-singapore" || len(dst.Tags) != 1 || dst.Host != "10.0.0.1" {
		t.Fatalf("unexpected %+v", dst)
	}
	if fillHostBlanks(&dst, &src) {
		t.Fatal("second fill should be no-op")
	}
}
