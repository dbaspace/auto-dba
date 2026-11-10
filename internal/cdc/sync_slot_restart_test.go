package cdc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestRestartDebeziumTask0(t *testing.T) {
	fastConnectTimers(t)
	rec := &connectRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if r.Method == http.MethodPost && r.URL.Path == "/connectors/c1/tasks/0/restart" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	if err := RestartDebeziumTask0(context.Background(), srv.URL, "c1"); err != nil {
		t.Fatal(err)
	}
	if rec.count("/connectors/c1/restart") != 0 {
		t.Fatalf("must not call connector includeTasks restart: %v", rec.calls)
	}
	if rec.count("/tasks/0/restart") != 1 {
		t.Fatalf("want tasks/0 once: %v", rec.calls)
	}
}

func TestRestartDebeziumTask0_409Retries(t *testing.T) {
	fastConnectTimers(t)
	rec := &connectRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if rec.count("/tasks/0/restart") <= 2 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"rebalance"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	if err := RestartDebeziumTask0(context.Background(), srv.URL, "c1"); err != nil {
		t.Fatal(err)
	}
	if n := rec.count("/tasks/0/restart"); n != 3 {
		t.Fatalf("want 3 attempts, got %d: %v", n, rec.calls)
	}
}

func TestRestartDialHostUsesConnectionHost(t *testing.T) {
	inst := &store.Instance{
		ID: "i1", Host: "pg.bj.restosuite.cn", Port: 5432,
		Domains: []string{"pg.report.bj.restosuite.cn", "pg.bill.bj.restosuite.cn"},
	}
	got, err := CollectHost(inst)
	if err != nil || got != "pg.bj.restosuite.cn" {
		t.Fatalf("dial %q err %v", got, err)
	}
	if got == "pg.report.bj.restosuite.cn" {
		t.Fatal("must dial connection host, not the database domain")
	}
}

func TestFillSlotBoundHostUsesRegisteredDomain(t *testing.T) {
	insts := []store.Instance{{
		ID: "i1", Host: "pg.bj.restosuite.cn", Port: 5432,
		Domains: []string{"pg.report.bj.restosuite.cn", "pg.bill.bj.restosuite.cn"},
	}}
	dbs := []store.Database{{
		InstanceID: "i1", DBName: "db_report", Domains: []string{"pg.report.bj.restosuite.cn"},
	}}
	slot := store.SyncSlot{Host: "pg.bj.restosuite.cn", Port: 5432, Database: "db_report", SlotName: "bj_slot_db_report"}
	rows := FillSlotBoundHosts([]SlotRow{{SyncSlot: slot}}, insts, dbs)
	if rows[0].BoundHost != "pg.report.bj.restosuite.cn" || rows[0].Host != "pg.bj.restosuite.cn" {
		t.Fatalf("display=%q stored=%q", rows[0].BoundHost, rows[0].Host)
	}
	bare := FillSlotBoundHosts([]SlotRow{{SyncSlot: store.SyncSlot{Host: slot.Host, Port: slot.Port, Database: "db_report"}}}, insts, []store.Database{{
		InstanceID: "i1", DBName: "db_report",
	}})
	if bare[0].BoundHost != "" {
		t.Fatalf("empty binding must stay empty, got %q", bare[0].BoundHost)
	}
	tagged := FillSlotBoundHosts([]SlotRow{{SyncSlot: store.SyncSlot{Host: slot.Host, Port: slot.Port, Database: "db_invoice"}}}, insts, []store.Database{{
		InstanceID: "i1", DBName: "db_invoice", Tags: []string{"idc=beijing", "domain=pg.invoice.bj.restosuite.cn"},
	}})
	if tagged[0].BoundHost != "pg.invoice.bj.restosuite.cn" {
		t.Fatalf("tag domain %q", tagged[0].BoundHost)
	}
	missing := FillSlotBoundHosts([]SlotRow{{SyncSlot: store.SyncSlot{Host: slot.Host, Port: slot.Port, Database: "db_other"}}}, insts, dbs)
	if missing[0].BoundHost != "" {
		t.Fatalf("unregistered db %q", missing[0].BoundHost)
	}
}

func TestFindInstanceForSlotByListedDomain(t *testing.T) {
	insts := []store.Instance{{
		ID: "i1", Host: "10.0.0.8", Port: 5432, Domains: []string{"pg.bj.restosuite.cn"},
	}}
	slot := store.SyncSlot{Host: "pg.bj.restosuite.cn", Port: 5432, SlotName: "s"}
	got := FindInstanceForSlot(slot, insts)
	if got == nil || got.ID != "i1" {
		t.Fatalf("domain host = %+v", got)
	}
	if got.Host != "10.0.0.8" {
		t.Fatalf("instance host changed: %s", got.Host)
	}
}

func TestRestartDebeziumTask0_EmptyName(t *testing.T) {
	err := RestartDebeziumTask0(context.Background(), "http://x", "  ")
	if err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("want empty name error, got %v", err)
	}
}
