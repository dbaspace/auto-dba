package cdc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func fastConnectTimers(t *testing.T) {
	t.Helper()
	oldBackoff, oldPoll := connectRestartBackoff, connectWaitPoll
	connectRestartBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	connectWaitPoll = 5 * time.Millisecond
	t.Cleanup(func() { connectRestartBackoff, connectWaitPoll = oldBackoff, oldPoll })
}

type connectRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *connectRecorder) add(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := req.Method + " " + req.URL.Path
	if req.URL.RawQuery != "" {
		s += "?" + req.URL.RawQuery
	}
	r.calls = append(r.calls, s)
}

func (r *connectRecorder) count(sub string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

const twoTaskStatus = `{"name":"c1","connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"RUNNING"},{"id":1,"state":"RUNNING"}]}`

func TestConnectRestartIncludeTasks202(t *testing.T) {
	fastConnectTimers(t)
	rec := &connectRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if r.Method == http.MethodPost && r.URL.Path == "/connectors/c1/restart" {
			if r.URL.Query().Get("includeTasks") != "true" || r.URL.Query().Get("onlyFailed") != "false" {
				t.Errorf("missing includeTasks/onlyFailed: %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	if err := connectRestartConnectorAndTasks(context.Background(), srv.URL, "c1"); err != nil {
		t.Fatal(err)
	}
	if rec.count("/tasks/") != 0 || rec.count("/status") != 0 {
		t.Fatalf("202 should not fall back to per-task restart: %v", rec.calls)
	}
}

func TestConnectRestart204FallsBackToTasks(t *testing.T) {
	fastConnectTimers(t)
	rec := &connectRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/connectors/c1/restart":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/connectors/c1/status":
			_, _ = w.Write([]byte(twoTaskStatus))
		case r.Method == http.MethodPost && (r.URL.Path == "/connectors/c1/tasks/0/restart" || r.URL.Path == "/connectors/c1/tasks/1/restart"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := connectRestartConnectorAndTasks(context.Background(), srv.URL, "c1"); err != nil {
		t.Fatal(err)
	}
	if rec.count("/tasks/0/restart") != 1 || rec.count("/tasks/1/restart") != 1 {
		t.Fatalf("want each task restarted once: %v", rec.calls)
	}
}

func TestConnectRestart409RetriesThenOK(t *testing.T) {
	fastConnectTimers(t)
	rec := &connectRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		if rec.count("/restart") <= 2 {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"rebalance in progress"}`))
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	if err := connectRestartConnectorAndTasks(context.Background(), srv.URL, "c1"); err != nil {
		t.Fatal(err)
	}
	if n := rec.count("/restart"); n != 3 {
		t.Fatalf("want 3 attempts, got %d", n)
	}
}

func TestConnectRestartOtherErrorNoRetry(t *testing.T) {
	fastConnectTimers(t)
	rec := &connectRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`boom`))
	}))
	defer srv.Close()
	err := connectRestartConnectorAndTasks(context.Background(), srv.URL, "c1")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("want 500 error, got %v", err)
	}
	if n := rec.count("/restart"); n != 1 {
		t.Fatalf("500 should not retry, got %d attempts", n)
	}
}

func TestConnectWaitRunning(t *testing.T) {
	fastConnectTimers(t)
	serve := func(bodies ...string) *httptest.Server {
		var mu sync.Mutex
		i := 0
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			b := bodies[min(i, len(bodies)-1)]
			i++
			mu.Unlock()
			_, _ = w.Write([]byte(b))
		}))
	}

	srv := serve(
		`{"connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"RESTARTING"}]}`,
		`{"connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"RUNNING"}]}`,
	)
	if err := connectWaitRunning(context.Background(), srv.URL, "c1", time.Second); err != nil {
		t.Fatalf("running: %v", err)
	}
	srv.Close()

	srv = serve(`{"connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"FAILED","trace":"org.postgresql.util.PSQLException: replication slot is active\n\tat x"}]}`)
	err := connectWaitRunning(context.Background(), srv.URL, "c1", time.Second)
	if err == nil || errors.Is(err, errNotSettled) || !strings.Contains(err.Error(), "replication slot is active") || strings.Contains(err.Error(), "\tat") {
		t.Fatalf("failed: want trace first line, got %v", err)
	}
	srv.Close()

	srv = serve(`{"connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"UNASSIGNED"}]}`)
	if err := connectWaitRunning(context.Background(), srv.URL, "c1", 30*time.Millisecond); !errors.Is(err, errNotSettled) {
		t.Fatalf("timeout: want errNotSettled, got %v", err)
	}
	srv.Close()
}
