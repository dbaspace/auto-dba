package cdc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func probeTask(t *testing.T, base, status string, extra map[string]string) *store.CDCTask {
	t.Helper()
	if extra == nil {
		extra = map[string]string{}
	}
	extra["connect_base_url"] = base
	if extra["connector_name"] == "" {
		extra["connector_name"] = "conn_a"
	}
	b, _ := json.Marshal(extra)
	return &store.CDCTask{ID: "t-" + status, TaskType: "init", Status: status, ApprovalStatus: store.StatusApproved, Description: "-- auto-dba-cdc:" + string(b)}
}

func TestTaskConnectorPresence(t *testing.T) {
	fc := &fakeConnect{exists: map[string]bool{"conn_a": true}}
	srv := httptest.NewServer(http.HandlerFunc(fc.handler))
	defer srv.Close()
	o := &Orchestrator{}
	ctx := context.Background()

	InvalidateConnectorList(srv.URL)
	if p := o.TaskConnectorPresence(ctx, probeTask(t, srv.URL, store.StatusRunning, nil)); p.State != PresencePresent {
		t.Fatalf("want PRESENT, got %+v", p)
	}
	missing := probeTask(t, srv.URL, store.StatusRunning, map[string]string{"connector_name": "conn_x"})
	if p := o.TaskConnectorPresence(ctx, missing); p.State != PresenceNotFound || len(p.Missing) != 1 || p.Missing[0] != "conn_x" {
		t.Fatalf("want NOT_FOUND conn_x, got %+v", p)
	}
	dead := "http://127.0.0.1:1"
	InvalidateConnectorList(dead)
	if p := o.TaskConnectorPresence(ctx, probeTask(t, dead, store.StatusRunning, nil)); p.State != PresenceUnreachable {
		t.Fatalf("want UNREACHABLE, got %+v", p)
	}
}

func TestConnectGetStatus404IsNotFound(t *testing.T) {
	fc := &fakeConnect{exists: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(fc.handler))
	defer srv.Close()
	st, _ := connectGetStatus(context.Background(), srv.URL, "nope")
	if !ConnectorStatusNotFound(st) {
		t.Fatalf("404 should be NOT_FOUND: %v", st)
	}
	fc.exists["yes"] = true
	st, err := connectGetStatus(context.Background(), srv.URL, "yes")
	if err != nil || ConnectorStatusNotFound(st) {
		t.Fatalf("existing connector: %v %v", st, err)
	}
}

func TestReinitEligibleAndCanReinit(t *testing.T) {
	nf := ConnectorPresence{State: PresenceNotFound}
	present := ConnectorPresence{State: PresencePresent}
	unreach := ConnectorPresence{State: PresenceUnreachable}
	for _, st := range []string{store.StatusRunning, store.StatusPaused, store.StatusFailed} {
		task := &store.CDCTask{TaskType: "init", Status: st, ApprovalStatus: store.StatusApproved}
		if !CanReinit(task, nf) {
			t.Fatalf("%s + NOT_FOUND should be reinit-able", st)
		}
		if CanReinit(task, present) || CanReinit(task, unreach) {
			t.Fatalf("%s: only NOT_FOUND allows reinit", st)
		}
	}
	rejects := []*store.CDCTask{
		{TaskType: "add_table", Status: store.StatusRunning},
		{TaskType: "init", ParentID: "p", Status: store.StatusRunning},
		{TaskType: "init", Status: store.StatusPending, ApprovalStatus: store.StatusPending},
		{TaskType: "init", Status: store.StatusRunning, ApprovalStatus: store.StatusPending},
		{TaskType: "init", Status: store.StatusDeleted},
		{TaskType: "init", Status: store.StatusReinitializing},
		{TaskType: "init", Status: "creating"},
	}
	for _, task := range rejects {
		if ok, _ := ReinitEligibleStatus(task); ok {
			t.Fatalf("should reject %+v", task)
		}
	}
}

func TestReinitGate(t *testing.T) {
	fc := &fakeConnect{exists: map[string]bool{"conn_a": true}}
	srv := httptest.NewServer(http.HandlerFunc(fc.handler))
	defer srv.Close()
	o := &Orchestrator{}
	ctx := context.Background()
	InvalidateConnectorList(srv.URL)

	if err := o.ReinitGate(ctx, probeTask(t, srv.URL, store.StatusRunning, nil), ""); err == nil || !strings.Contains(err.Error(), "仍存在") {
		t.Fatalf("present connector must be rejected: %v", err)
	}
	if err := o.ReinitGate(ctx, probeTask(t, srv.URL, store.StatusRunning, map[string]string{"connector_name": "conn_x"}), ""); err != nil {
		t.Fatalf("missing connector should pass: %v", err)
	}
	retry := probeTask(t, srv.URL, store.StatusFailed, map[string]string{"error_step": "reinit"})
	if err := o.ReinitGate(ctx, retry, ""); err != nil {
		t.Fatalf("retry after reinit failure must skip NOT_FOUND gate: %v", err)
	}
	if err := o.ReinitGate(ctx, probeTask(t, srv.URL, "creating", map[string]string{"connector_name": "conn_x"}), ""); err == nil {
		t.Fatal("creating status must be rejected")
	}
}

func TestAnnotateReinit(t *testing.T) {
	fc := &fakeConnect{exists: map[string]bool{"conn_a": true}}
	srv := httptest.NewServer(http.HandlerFunc(fc.handler))
	defer srv.Close()
	InvalidateConnectorList(srv.URL)
	list := []store.CDCTask{
		*probeTask(t, srv.URL, store.StatusRunning, nil),
		*probeTask(t, srv.URL, store.StatusPaused, map[string]string{"connector_name": "conn_x"}),
		*probeTask(t, srv.URL, store.StatusPending, map[string]string{"connector_name": "conn_y"}),
	}
	(&Orchestrator{}).AnnotateReinit(context.Background(), list)
	if list[0].CanReinit || list[0].ConnectorMissing {
		t.Fatal("present connector must not be reinit-able")
	}
	if !list[1].CanReinit || !list[1].ConnectorMissing {
		t.Fatal("missing connector on paused task should be reinit-able")
	}
	if list[2].CanReinit {
		t.Fatal("pending task must not be reinit-able")
	}
}
