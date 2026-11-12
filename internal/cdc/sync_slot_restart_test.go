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
