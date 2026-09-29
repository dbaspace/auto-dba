package ha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"auto-dba/internal/store"
)

func TestParseMHAAppConf(t *testing.T) {
	if ParseMHAAppConf("") != mhaDefaultConf {
		t.Fatal("default")
	}
	if ParseMHAAppConf("/etc/mha/x.cnf") != "/etc/mha/x.cnf" {
		t.Fatal("plain path")
	}
	if ParseMHAAppConf(`{"app_conf":"/etc/mha/a.cnf"}`) != "/etc/mha/a.cnf" {
		t.Fatal("json app_conf")
	}
	if ParseMHAAppConf(`{"conf":"/etc/mha/b.cnf"}`) != "/etc/mha/b.cnf" {
		t.Fatal("json conf")
	}
}

func TestHAAutoSwitchoverIncludesMHA(t *testing.T) {
	if !store.HAAutoSwitchoverMode("mha") {
		t.Fatal("mha should auto switch")
	}
	if !store.CompatibleHAController("mysql", "mha", "mha") {
		t.Fatal("compat")
	}
}

func TestMHAAgentPlannedSwitchDryHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/mha/switch/planned" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["new_master_host"] != "10.0.0.2" {
			t.Errorf("host=%v", body["new_master_host"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "ok"})
	}))
	defer srv.Close()

	err := mhaAgentPlannedSwitch(context.Background(), ControllerAuth{
		Kind: "mha", BaseURL: srv.URL, AppConf: "/etc/mha/app.cnf",
	}, &store.HACluster{Name: "c1"}, &store.Instance{Host: "10.0.0.2", Port: 3306})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMHAAgentStatusAndRepl(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/mha/status":
			_, _ = w.Write([]byte(`{"ok":true,"message":"status ok"}`))
		case "/v1/mha/repl":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "repl ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	auth := ControllerAuth{Kind: "mha", BaseURL: srv.URL, AppConf: "/etc/mha/app.cnf"}
	ok, detail := MHAAgentStatus(context.Background(), auth)
	if !ok || detail == "" {
		t.Fatalf("status ok=%v detail=%q", ok, detail)
	}
	rok, rdetail := MHAAgentReplOK(context.Background(), auth)
	if !rok {
		t.Fatalf("repl %v %s", rok, rdetail)
	}
}

func TestPlannedSwitchoverMHABranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()
	err := PlannedSwitchover(context.Background(), &store.HACluster{Name: "c", Mode: "mha"}, ControllerAuth{
		Kind: "mha", BaseURL: srv.URL,
	}, &store.Instance{Host: "h", Port: 3306})
	if err != nil {
		t.Fatal(err)
	}
}
