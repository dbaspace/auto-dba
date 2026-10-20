package cdc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestRestartDebeziumTask0_EmptyName(t *testing.T) {
	err := RestartDebeziumTask0(context.Background(), "http://x", "  ")
	if err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("want empty name error, got %v", err)
	}
}
