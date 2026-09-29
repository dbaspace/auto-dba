package cdc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"auto-dba/internal/store"
)

func TestResolveReinitSnapshotMode(t *testing.T) {
	cases := []struct {
		req, orig, want string
		wantErr         bool
	}{
		{"", "initial", "initial", false},
		{"", "", SnapshotNever, false},
		{"never", "initial", "never", false},
		{"INITIAL", "never", "initial", false},
		{"always", "never", "", true},
	}
	for _, c := range cases {
		got, err := ResolveReinitSnapshotMode(c.req, c.orig)
		if (err != nil) != c.wantErr || got != c.want {
			t.Fatalf("ResolveReinitSnapshotMode(%q,%q)=%q,%v want %q err=%v", c.req, c.orig, got, err, c.want, c.wantErr)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func shardExtra(t *testing.T) map[string]string {
	return map[string]string{
		"env":              "prod",
		"publication_name": "pub_a",
		"slot_name":        "slot_a",
		"connector_name":   "conn_a",
		"topic_prefix":     "prod_orders",
		"snapshot_mode":    "initial",
		"debezium_mode":    ModeAfterOnly,
		"shard_db_bindings": mustJSON(t, []ShardDbBinding{
			{DbName: "db_a", PublicationName: "pub_a", SlotName: "slot_a", ConnectorName: "conn_a"},
			{DbName: "db_b", PublicationName: "pub_b", SlotName: "slot_b", ConnectorName: "conn_b"},
		}),
		"mappings": mustJSON(t, []TableMapping{
			{SourceTables: []string{"t_order"}, TargetTable: "t_order"},
			{SourceTables: []string{"t_gone"}, TargetTable: "t_gone", Status: "deleted"},
			{SourceTables: []string{"t_only_b"}, TargetTable: "t_only_b", SourceDbScope: "explicit", SourceDbNames: []string{"db_b"}},
		}),
		"debezium_config": mustJSON(t, map[string]string{
			"database.password":     MaskedPassword,
			"database.dbname":       "db_a",
			"snapshot.mode":         "initial",
			"table.include.list":    "public.stale",
			"heartbeat.interval.ms": "5000",
			"name":                  "conn_a",
		}),
	}
}

func TestBuildConnectorPlansPerBindingWithRealPassword(t *testing.T) {
	plans, err := buildConnectorPlans(connectorPlanInput{
		Extra: shardExtra(t), SourceDB: "db_a",
		PGHost: "10.0.0.1", PGPort: 5433, PGUser: "repl", PGPassword: "s3cret", SnapshotMode: "never",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("want 2 plans, got %d", len(plans))
	}
	byName := map[string]ConnectorPlan{}
	for _, p := range plans {
		byName[p.ConnectorName] = p
		if p.Config["database.password"] != "s3cret" {
			t.Fatalf("%s password=%q", p.ConnectorName, p.Config["database.password"])
		}
		if p.Config["snapshot.mode"] != "never" {
			t.Fatalf("%s snapshot=%q", p.ConnectorName, p.Config["snapshot.mode"])
		}
		if p.Config["heartbeat.interval.ms"] != "5000" {
			t.Fatalf("%s template key lost", p.ConnectorName)
		}
		if _, ok := p.Config["name"]; ok {
			t.Fatalf("%s config must not carry name", p.ConnectorName)
		}
		if p.Config["database.hostname"] != "10.0.0.1" || p.Config["database.port"] != "5433" {
			t.Fatalf("%s host/port not from instance: %v", p.ConnectorName, p.Config)
		}
	}
	a, b := byName["conn_a"], byName["conn_b"]
	if a.Config["database.dbname"] != "db_a" || a.Config["slot.name"] != "slot_a" || a.Config["publication.name"] != "pub_a" {
		t.Fatalf("conn_a identity wrong: %v", a.Config)
	}
	if b.Config["database.dbname"] != "db_b" || b.Config["slot.name"] != "slot_b" || b.Config["publication.name"] != "pub_b" {
		t.Fatalf("conn_b identity wrong: %v", b.Config)
	}
	if a.Config["table.include.list"] != "public.t_order" {
		t.Fatalf("conn_a include=%q", a.Config["table.include.list"])
	}
	if b.Config["table.include.list"] != "public.t_only_b,public.t_order" {
		t.Fatalf("conn_b include=%q", b.Config["table.include.list"])
	}
	if strings.Contains(a.Config["table.include.list"], "t_gone") {
		t.Fatal("deleted mapping must be excluded")
	}
}

func TestBuildConnectorPlansDefaultsToOriginalSnapshot(t *testing.T) {
	extra := shardExtra(t)
	snap, _ := ResolveReinitSnapshotMode("", extra["snapshot_mode"])
	plans, err := buildConnectorPlans(connectorPlanInput{Extra: extra, SourceDB: "db_a", PGHost: "h", PGUser: "u", SnapshotMode: snap})
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].Config["snapshot.mode"] != "initial" {
		t.Fatalf("snapshot=%q", plans[0].Config["snapshot.mode"])
	}
}

func TestBuildConnectorPlansNoMappings(t *testing.T) {
	_, err := buildConnectorPlans(connectorPlanInput{Extra: map[string]string{"publication_name": "p", "slot_name": "s"}})
	if err == nil || !strings.Contains(err.Error(), "没有有效的表映射") {
		t.Fatalf("err=%v", err)
	}
}

func TestHubConnectorCopyMasksAndKeepsOriginalSnapshot(t *testing.T) {
	extra := shardExtra(t)
	plans, err := buildConnectorPlans(connectorPlanInput{Extra: extra, SourceDB: "db_a", PGHost: "h", PGUser: "u", PGPassword: "s3cret", SnapshotMode: "never"})
	if err != nil {
		t.Fatal(err)
	}
	cp := hubConnectorCopy(plans, extra)
	if cp["database.password"] != MaskedPassword {
		t.Fatalf("password not masked: %q", cp["database.password"])
	}
	if cp["snapshot.mode"] != "initial" {
		t.Fatalf("hub copy snapshot=%q want original initial", cp["snapshot.mode"])
	}
	if cp["database.dbname"] != "db_a" {
		t.Fatalf("hub copy should be primary connector, got db=%q", cp["database.dbname"])
	}
	for _, c := range maskedPlanConfigs(plans) {
		if c.Config["database.password"] == "s3cret" {
			t.Fatal("preview configs leaked password")
		}
	}
}

func TestInitIncomplete(t *testing.T) {
	for step, want := range map[string]bool{"pg": true, "kafka": true, "debezium": true, "starrocks": true, "reinit": false, "": false} {
		if got := InitIncomplete(map[string]string{"error_step": step}); got != want {
			t.Fatalf("step %q: got %v", step, got)
		}
	}
	if !InitIncomplete(map[string]string{"init_starrocks_pending": "true", "error_step": "reinit"}) {
		t.Fatal("pending flag must win")
	}
}

func TestMissingPublicationTables(t *testing.T) {
	got := missingPublicationTables("public", []string{"a", "b", "sales.c"}, []string{"a", "sales.c"})
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("got %v", got)
	}
}

func TestKafkaHostSetOrderInsensitive(t *testing.T) {
	if kafkaHostSet("k2:9092, k1:9092") != kafkaHostSet("K1:9092,k2:9092") {
		t.Fatal("kafka host sets should match regardless of order/case")
	}
}

type fakeConnect struct {
	mu      sync.Mutex
	exists  map[string]bool
	methods []string
}

func (f *fakeConnect) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.methods = append(f.methods, r.Method+" "+r.URL.Path)
	path := strings.TrimPrefix(r.URL.Path, "/connectors")
	switch {
	case r.Method == http.MethodGet && path == "":
		names := []string{}
		for n := range f.exists {
			names = append(names, n)
		}
		_ = json.NewEncoder(w).Encode(names)
	case r.Method == http.MethodPost && path == "":
		var env struct {
			Name string `json:"name"`
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &env)
		f.exists[env.Name] = true
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	default:
		parts := strings.Split(strings.Trim(path, "/"), "/")
		name := parts[0]
		if !f.exists[name] && r.Method != http.MethodPut {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error_code":404,"message":"Connector ` + name + ` not found"}`))
			return
		}
		switch {
		case len(parts) == 1:
			_, _ = w.Write([]byte(`{"name":"` + name + `"}`))
		case parts[1] == "config" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"database.password":"old"}`))
		case parts[1] == "config" && r.Method == http.MethodPut:
			f.exists[name] = true
			_, _ = w.Write([]byte(`{}`))
		case parts[1] == "status":
			_, _ = w.Write([]byte(`{"connector":{"state":"RUNNING"},"tasks":[]}`))
		}
	}
}

func TestReinitConnectorsPutsExistingAndCreatesMissing(t *testing.T) {
	fc := &fakeConnect{exists: map[string]bool{"conn_a": true}}
	srv := httptest.NewServer(http.HandlerFunc(fc.handler))
	defer srv.Close()
	o := &Orchestrator{}
	extra := shardExtra(t)
	plans, err := buildConnectorPlans(connectorPlanInput{Extra: extra, SourceDB: "db_a", PGHost: "h", PGUser: "u", PGPassword: "pw", SnapshotMode: "never"})
	if err != nil {
		t.Fatal(err)
	}
	task := &store.CDCTask{ID: "t1", TaskType: "init"}
	if err := o.reinitConnectors(context.Background(), task, extra, srv.URL, plans, "tester", ""); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(fc.methods, "\n")
	if !strings.Contains(joined, "PUT /connectors/conn_a/config") {
		t.Fatalf("existing connector should be PUT:\n%s", joined)
	}
	if !strings.Contains(joined, "POST /connectors") || !fc.exists["conn_b"] {
		t.Fatalf("missing connector should be created:\n%s", joined)
	}
	if strings.Contains(extra["debezium_config"], `"pw"`) {
		t.Fatal("hub copy must not contain real password")
	}
}
