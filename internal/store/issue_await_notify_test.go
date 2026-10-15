package store

import (
	"testing"
	"time"
)

func TestIssueAwaitStuckAlreadyNotified(t *testing.T) {
	if IssueAwaitStuckAlreadyNotified(nil) {
		t.Fatal("nil")
	}
	if IssueAwaitStuckAlreadyNotified(&Issue{}) {
		t.Fatal("empty")
	}
	it := &Issue{NeedRollback: `{"_adba_notify":{"await_exec_stuck_at":"2026-01-01T00:00:00Z"}}`}
	if !IssueAwaitStuckAlreadyNotified(it) {
		t.Fatal("need_rollback marker")
	}
	it2 := &Issue{ExecuteResult: `{"_notify":{"await_stuck":"t"}}`}
	if !IssueAwaitStuckAlreadyNotified(it2) {
		t.Fatal("exec marker")
	}
	it3 := &Issue{NeedRollback: `{"data_source_ids":["a"]}`}
	if IssueAwaitStuckAlreadyNotified(it3) {
		t.Fatal("unrelated json")
	}
}

func TestScheduledAtDueOrEmpty(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	if !ScheduledAtDueOrEmpty("", now) {
		t.Fatal("empty due")
	}
	past := now.Add(-time.Hour).UTC().Format(time.RFC3339)
	if !ScheduledAtDueOrEmpty(past, now) {
		t.Fatal("past due")
	}
	future := now.Add(2 * time.Hour).UTC().Format(time.RFC3339)
	if ScheduledAtDueOrEmpty(future, now) {
		t.Fatal("future not due")
	}
}

func TestNotifyConfigPublicFields(t *testing.T) {
	c := &NotifyConfig{
		EmailOnTicketExecFailed: true,
		EmailOnTicketAwaitExec:  true,
		EmailOnTicketAwaitStuck: true,
		AwaitExecStuckHours:     3,
		RobotsOnTicketCreate:    true,
		RobotRoutes:             []RobotRoute{{Index: 0, For: "ticket"}},
	}
	p := c.Public()
	if p["email_on_ticket_exec_failed"] != true {
		t.Fatal("exec failed flag")
	}
	if p["await_exec_stuck_hours"] != 3 {
		t.Fatalf("hours=%v", p["await_exec_stuck_hours"])
	}
	if p["robots_on_ticket_create"] != true {
		t.Fatal("robots ticket flag")
	}
	routes, ok := p["robot_routes"].([]RobotRoute)
	if !ok || len(routes) != 1 || routes[0].For != "ticket" {
		t.Fatalf("robot_routes=%v", p["robot_routes"])
	}
	if DefaultNotifyConfig().AwaitStuckHours() != DefaultAwaitExecStuckHours {
		t.Fatal("default hours")
	}
}

func TestMergeAdbaNotifyRoundTrip(t *testing.T) {
	merged, ok := mergeAdbaNotify(`{"data_source_ids":["a"]}`, awaitStuckAtKey, "ts")
	if !ok {
		t.Fatal("merge")
	}
	it := &Issue{NeedRollback: merged}
	if !IssueAwaitStuckAlreadyNotified(it) {
		t.Fatal("detect")
	}
	if _, ok := mergeAdbaNotify("plain-text", awaitStuckAtKey, "x"); ok {
		t.Fatal("non-json")
	}
}
