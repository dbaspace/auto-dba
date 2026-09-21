package cdc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeConnectBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"http://10.100.112.17:8085", "http://10.100.112.17:8085"},
		{"http://10.100.112.17:8085/", "http://10.100.112.17:8085"},
		{"https://connect.example.com:8083/", "https://connect.example.com:8083"},
		{"10.100.112.17:8085", "http://10.100.112.17:8085"},
		{"connect.example.com", "http://connect.example.com:8083"},
	}
	for _, c := range cases {
		if got := NormalizeConnectBaseURL(c.in); got != c.want {
			t.Fatalf("NormalizeConnectBaseURL(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestResolveConnectBaseURL(t *testing.T) {
	got := ResolveConnectBaseURL("", "", "10.100.112.17:8085", "http://unused:8083")
	if got != "http://10.100.112.17:8085" {
		t.Fatalf("got %q", got)
	}
	got = ResolveConnectBaseURL("http://task:8085/", "http://worker:1", "host:2", "http://cfg:3")
	if got != "http://task:8085" {
		t.Fatalf("override should win, got %q", got)
	}
}

func TestEnsureDebeziumConnectorCreateThenUpdate(t *testing.T) {
	var posts, puts, gets int
	var lastPost, lastPut map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/connectors/"):
			gets++
			if puts == 0 && posts == 0 {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "test_db_ai", "config": map[string]string{"database.dbname": "db"}})
		case r.Method == http.MethodPost && r.URL.Path == "/connectors":
			posts++
			_ = json.Unmarshal(body, &lastPost)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"name":"test_db_ai"}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/config"):
			puts++
			_ = json.Unmarshal(body, &lastPut)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := map[string]string{"database.dbname": "db", "database.password": "secret"}
	if err := EnsureDebeziumConnector(context.Background(), srv.URL, "test_db_ai", cfg); err != nil {
		t.Fatalf("create: %v", err)
	}
	if posts != 1 {
		t.Fatalf("want 1 POST, got %d (gets=%d)", posts, gets)
	}
	env, _ := lastPost["config"].(map[string]any)
	if env["database.password"] != "secret" {
		t.Fatalf("POST should send real password, got %#v", lastPost)
	}

	if err := EnsureDebeziumConnector(context.Background(), srv.URL, "test_db_ai", cfg); err != nil {
		t.Fatalf("update: %v", err)
	}
	if puts != 1 {
		t.Fatalf("want 1 PUT on existing connector, got %d", puts)
	}
	if lastPut["database.password"] != "secret" {
		t.Fatalf("PUT body should be flat config, got %#v", lastPut)
	}
}

func TestEnsureDebeziumConnectorConflictRetriesPut(t *testing.T) {
	var puts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			http.NotFound(w, r)
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"already exists"}`))
		case r.Method == http.MethodPut:
			puts++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	if err := EnsureDebeziumConnector(context.Background(), srv.URL, "dup", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("409 should fall back to PUT: %v", err)
	}
	if puts != 1 {
		t.Fatalf("want 1 PUT after 409, got %d", puts)
	}
}

func TestOrchestratorResolveConnectBaseIgnoresGlobalEnabled(t *testing.T) {
	o := &Orchestrator{}
	o.Cfg.Enabled = false
	o.Cfg.DebeziumURL = ""
	got := o.ResolveConnectBase(map[string]string{"connect_base_url": "10.100.112.17:8085"})
	if got != "http://10.100.112.17:8085" {
		t.Fatalf("should use task extra even when cdc.enabled=false, got %q", got)
	}
	// 无地址时 Action 门禁应走连接器提示，而非仅因 enabled=false
	if empty := o.ResolveConnectBase(nil); empty != "" {
		t.Fatalf("want empty base without connector/yaml, got %q", empty)
	}
}
