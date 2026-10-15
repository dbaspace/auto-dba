package notify

import (
	"testing"

	"auto-dba/internal/store"
)

func TestRobotMatchesFor(t *testing.T) {
	cases := []struct {
		route, want string
		ok          bool
	}{
		{"", "alert", true},
		{"alert", "alert", true},
		{"alert", "ticket", false},
		{"ticket", "ticket", true},
		{"ticket", "alert", false},
		{"both", "alert", true},
		{"both", "ticket", true},
		{"ALERT", "alert", true},
	}
	for _, c := range cases {
		if got := robotMatchesFor(c.route, c.want); got != c.ok {
			t.Fatalf("route=%q want=%q got=%v expect=%v", c.route, c.want, got, c.ok)
		}
	}
}

func TestRouteForIndex(t *testing.T) {
	routes := []store.RobotRoute{
		{Index: 0, For: "ticket"},
		{Index: 2, For: "both"},
	}
	if routeForIndex(routes, 0) != "ticket" {
		t.Fatal("0")
	}
	if routeForIndex(routes, 1) != "alert" {
		t.Fatal("default")
	}
	if routeForIndex(routes, 2) != "both" {
		t.Fatal("2")
	}
}

func TestSanitizeRobotRoutes(t *testing.T) {
	out := store.SanitizeRobotRoutes([]store.RobotRoute{
		{Index: 1, For: "TICKET"},
		{Index: 1, For: "alert"},
		{Index: -1, For: "both"},
		{Index: 0, For: "weird"},
	})
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Index != 1 || out[0].For != "ticket" {
		t.Fatalf("first=%+v", out[0])
	}
	if out[1].Index != 0 || out[1].For != "alert" {
		t.Fatalf("second=%+v", out[1])
	}
}
