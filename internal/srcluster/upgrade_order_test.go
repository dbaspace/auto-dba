package srcluster

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestOrderUpgradeNodesBEThenFollowerThenLeader(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "fe1", Component: "fe", Host: "10.0.0.1", IsPrimaryFE: true},
		{ID: "fe2", Component: "fe", Host: "10.0.0.2"},
		{ID: "fe3", Component: "fe", Host: "10.0.0.3"},
		{ID: "be1", Component: "be", Host: "10.0.0.4"},
		{ID: "be2", Component: "be", Host: "10.0.0.5"},
	}
	// 实际 leader 是 fe2（非 is_primary_fe）
	bes, followers, leader, src := orderUpgradeNodes(nodes, "10.0.0.2")
	if src != "show_proc" {
		t.Fatalf("src=%s", src)
	}
	if len(bes) != 2 || bes[0].ID != "be1" {
		t.Fatalf("bes=%v", bes)
	}
	if leader == nil || leader.ID != "fe2" {
		t.Fatalf("leader=%v", leader)
	}
	var fids []string
	for _, f := range followers {
		fids = append(fids, f.ID)
	}
	joined := strings.Join(fids, ",")
	if strings.Contains(joined, "fe2") {
		t.Fatalf("leader must not be in followers: %s", joined)
	}
	if !strings.Contains(joined, "fe1") || !strings.Contains(joined, "fe3") {
		t.Fatalf("followers=%s", joined)
	}
}

func TestOrderUpgradeNodesFallbackPrimary(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "fe1", Component: "fe", Host: "10.0.0.1", IsPrimaryFE: true},
		{ID: "fe2", Component: "fe", Host: "10.0.0.2"},
		{ID: "be1", Component: "be", Host: "10.0.0.3"},
	}
	bes, followers, leader, src := orderUpgradeNodes(nodes, "")
	if src != "is_primary_fe" || leader == nil || leader.ID != "fe1" {
		t.Fatalf("leader=%v src=%s", leader, src)
	}
	if len(bes) != 1 || len(followers) != 1 || followers[0].ID != "fe2" {
		t.Fatalf("bes=%d followers=%v", len(bes), followers)
	}
}

func TestOrderUpgradeNodesSingleFE(t *testing.T) {
	nodes := []store.SrNode{
		{ID: "fe1", Component: "fe", Host: "10.0.0.1", IsPrimaryFE: true},
		{ID: "be1", Component: "be", Host: "10.0.0.2"},
	}
	_, followers, leader, _ := orderUpgradeNodes(nodes, "10.0.0.1")
	if len(followers) != 0 || leader == nil || leader.ID != "fe1" {
		t.Fatalf("followers=%d leader=%v", len(followers), leader)
	}
}
