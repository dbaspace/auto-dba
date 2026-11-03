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

type addTableConnectStub struct {
	mu       sync.Mutex
	configs  map[string]map[string]string
	puts     []string
	putBodies map[string]map[string]string
	calls    []string
}

func (s *addTableConnectStub) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	path := strings.TrimPrefix(r.URL.Path, "/connectors/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	name := parts[0]
	switch {
	case len(parts) >= 2 && parts[1] == "config" && r.Method == http.MethodGet:
		cfg := s.configs[name]
		if cfg == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(cfg)
	case len(parts) >= 2 && parts[1] == "config" && r.Method == http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		cfg := ParseConfigMap(string(body))
		s.puts = append(s.puts, name)
		if s.putBodies == nil {
			s.putBodies = map[string]map[string]string{}
		}
		s.putBodies[name] = cfg
		s.configs[name] = cfg
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	case len(parts) >= 2 && parts[1] == "restart" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusAccepted)
	case len(parts) >= 2 && parts[1] == "status" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte(`{"connector":{"state":"RUNNING"},"tasks":[{"id":0,"state":"RUNNING"}]}`))
	case len(parts) >= 3 && parts[1] == "tasks" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *addTableConnectStub) putCount(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, p := range s.puts {
		if p == name {
			n++
		}
	}
	return n
}

func (s *addTableConnectStub) lastPut(name string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putBodies[name]
}

func TestStepAddTableDebeziumUsesParentConnectorNotChildExtra(t *testing.T) {
	fastConnectTimers(t)
	stub := &addTableConnectStub{
		configs: map[string]map[string]string{
			"ai_beijing_db_bill_29": {
				"table.include.list":     "public.tbl_heartbeat_table",
				"transforms":             "unwrap",
				"transforms.unwrap.type": "io.debezium.transforms.ExtractNewRecordState",
			},
			"ai_beijing_db_bill_7": {
				"table.include.list":     "public.tbl_heartbeat_table",
				"transforms":             "unwrap",
				"transforms.unwrap.type": "io.debezium.transforms.ExtractNewRecordState",
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer srv.Close()

	parentExtra := map[string]string{
		"connect_base_url":   srv.URL,
		"publication_name":   "ai_beijing_db_bill_29",
		"slot_name":          "slot_ai_beijing_db_bill_29",
		"connector_name":     "ai_beijing_db_bill_29",
		"source_schema":      "public",
		"env":                "beijing",
		"debezium_config":    MarshalConfigMap(stub.configs["ai_beijing_db_bill_29"]),
	}
	childExtra := map[string]string{
		// 复现串用：子任务带了 _7 的链路名
		"publication_name": "ai_beijing_db_bill_7",
		"slot_name":        "slot_ai_beijing_db_bill_7",
		"connector_name":   "ai_beijing_db_bill_7",
		"mappings": mustJSON(t, []TableMapping{{
			IsShardGroup: true,
			TargetTable:  "tbl_bill_detail",
			SourceTables: []string{"tbl_bill_detail_0", "tbl_bill_detail_1", "tbl_bill_detail_63"},
		}}),
	}
	tables := []string{"tbl_bill_detail_0", "tbl_bill_detail_1", "tbl_bill_detail_63"}
	parent := &store.CDCTask{ID: "parent-29", SourceDB: "db_bill_29", TaskType: "init"}
	child := &store.CDCTask{ID: "child-add", ParentID: parent.ID, TaskType: "add_table"}

	o := &Orchestrator{}
	if err := o.stepAddTableDebezium(context.Background(), child, parent, childExtra, parentExtra, tables); err != nil {
		t.Fatal(err)
	}
	if stub.putCount("ai_beijing_db_bill_29") != 1 {
		t.Fatalf("want PUT on _29 once, puts=%v calls=%v", stub.puts, stub.calls)
	}
	if stub.putCount("ai_beijing_db_bill_7") != 0 {
		t.Fatalf("must not touch _7 connector, puts=%v", stub.puts)
	}
	cfg := stub.lastPut("ai_beijing_db_bill_29")
	if cfg == nil {
		t.Fatal("missing put body")
	}
	include := cfg["table.include.list"]
	wantRegex := "public.tbl_bill_detail(_[0-9]+|[0-9]+)"
	if !strings.Contains(include, wantRegex) {
		t.Fatalf("want regex %q in include=%q", wantRegex, include)
	}
	if !strings.Contains(include, "public.tbl_heartbeat_table") {
		t.Fatalf("heartbeat lost: %s", include)
	}
	if cfg["transforms.tbl_bill_detail.type"] != debeziumTransformByLogicalTableRouter {
		t.Fatalf("type=%q", cfg["transforms.tbl_bill_detail.type"])
	}
	if cfg["transforms.tbl_bill_detail.topic.regex"] != "(.*)tbl_bill_detail(_[0-9]+|[0-9]+)" {
		t.Fatalf("topic.regex=%q", cfg["transforms.tbl_bill_detail.topic.regex"])
	}
	if cfg["transforms.tbl_bill_detail.topic.replacement"] != "$1tbl_bill_detail" {
		t.Fatalf("topic.replacement=%q", cfg["transforms.tbl_bill_detail.topic.replacement"])
	}
	if cfg["transforms.tbl_bill_detail.key.field.name"] != "source_table" {
		t.Fatalf("key.field.name=%q", cfg["transforms.tbl_bill_detail.key.field.name"])
	}
	chain := cfg["transforms"]
	if !strings.Contains(chain, "tbl_bill_detail") {
		t.Fatalf("transforms missing router: %q", chain)
	}
	if !strings.HasSuffix(chain, "unwrap") && !strings.Contains(chain, ",unwrap") && chain != "unwrap" {
		// unwrap 应在链末尾
		parts := strings.Split(chain, ",")
		if parts[len(parts)-1] != "unwrap" {
			t.Fatalf("unwrap should be last: %q", chain)
		}
	}
}

func TestStepAddTableDebeziumFiltersMappingsPerDB(t *testing.T) {
	fastConnectTimers(t)
	stub := &addTableConnectStub{
		configs: map[string]map[string]string{
			"conn_a": {
				"table.include.list":     "public.keep_a",
				"transforms":             "unwrap",
				"transforms.unwrap.type": "io.debezium.transforms.ExtractNewRecordState",
			},
			"conn_b": {
				"table.include.list":     "public.keep_b",
				"transforms":             "unwrap",
				"transforms.unwrap.type": "io.debezium.transforms.ExtractNewRecordState",
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer srv.Close()

	bindings, _ := json.Marshal([]ShardDbBinding{
		{DbName: "db_a", PublicationName: "pub_a", SlotName: "slot_a", ConnectorName: "conn_a"},
		{DbName: "db_b", PublicationName: "pub_b", SlotName: "slot_b", ConnectorName: "conn_b"},
	})
	parentExtra := map[string]string{
		"connect_base_url":   srv.URL,
		"shard_db":           "true",
		"source_scope_mode":  "multi_db",
		"source_db_names":    "db_a,db_b",
		"publication_name":   "pub_a",
		"slot_name":          "slot_a",
		"connector_name":     "conn_a",
		"source_schema":      "public",
		"shard_db_bindings":  string(bindings),
	}
	childExtra := map[string]string{
		"mappings": mustJSON(t, []TableMapping{{
			IsShardGroup:  true,
			TargetTable:   "orders",
			SourceTables:  []string{"orders_0", "orders_1"},
			SourceDbScope: "explicit",
			SourceDbNames: []string{"db_a"},
		}}),
	}
	parent := &store.CDCTask{ID: "p", SourceDB: "db_a", TaskType: "init"}
	child := &store.CDCTask{ID: "c", ParentID: parent.ID, TaskType: "add_table"}
	o := &Orchestrator{}
	if err := o.stepAddTableDebezium(context.Background(), child, parent, childExtra, parentExtra, []string{"orders_0", "orders_1"}); err != nil {
		t.Fatal(err)
	}
	if stub.putCount("conn_a") != 1 {
		t.Fatalf("want PUT conn_a, puts=%v", stub.puts)
	}
	if stub.putCount("conn_b") != 0 {
		t.Fatalf("db_b must not be updated, puts=%v", stub.puts)
	}
	cfg := stub.lastPut("conn_a")
	if !strings.Contains(cfg["table.include.list"], "public.orders(_[0-9]+|[0-9]+)") {
		t.Fatalf("include=%q", cfg["table.include.list"])
	}
	if cfg["transforms.orders.type"] != debeziumTransformByLogicalTableRouter {
		t.Fatalf("missing router on conn_a: %+v", cfg)
	}
	if stub.configs["conn_b"]["table.include.list"] != "public.keep_b" {
		t.Fatalf("conn_b include changed: %q", stub.configs["conn_b"]["table.include.list"])
	}
}

func TestStepAddTableDebeziumIdempotentNoPut(t *testing.T) {
	fastConnectTimers(t)
	existing := map[string]string{
		"table.include.list":                       "public.tbl_heartbeat_table,public.tbl_bill_detail(_[0-9]+|[0-9]+)",
		"transforms":                               "tbl_bill_detail,unwrap",
		"transforms.unwrap.type":                   "io.debezium.transforms.ExtractNewRecordState",
		"transforms.tbl_bill_detail.type":          debeziumTransformByLogicalTableRouter,
		"transforms.tbl_bill_detail.topic.regex":   "(.*)tbl_bill_detail(_[0-9]+|[0-9]+)",
		"transforms.tbl_bill_detail.topic.replacement": "$1tbl_bill_detail",
		"transforms.tbl_bill_detail.key.field.name":    "source_table",
	}
	stub := &addTableConnectStub{
		configs: map[string]map[string]string{
			"ai_beijing_db_bill_29": cloneStringMap(existing),
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer srv.Close()

	parentExtra := map[string]string{
		"connect_base_url": srv.URL,
		"publication_name": "ai_beijing_db_bill_29",
		"slot_name":        "slot_ai_beijing_db_bill_29",
		"connector_name":   "ai_beijing_db_bill_29",
		"source_schema":    "public",
	}
	childExtra := map[string]string{
		"mappings": mustJSON(t, []TableMapping{{
			IsShardGroup: true,
			TargetTable:  "tbl_bill_detail",
			SourceTables: []string{"tbl_bill_detail_0", "tbl_bill_detail_63"},
		}}),
	}
	parent := &store.CDCTask{ID: "p", SourceDB: "db_bill_29", TaskType: "init"}
	child := &store.CDCTask{ID: "c", ParentID: parent.ID, TaskType: "add_table"}
	o := &Orchestrator{}
	if err := o.stepAddTableDebezium(context.Background(), child, parent, childExtra, parentExtra,
		[]string{"tbl_bill_detail_0", "tbl_bill_detail_63"}); err != nil {
		t.Fatal(err)
	}
	if len(stub.puts) != 0 {
		t.Fatalf("idempotent merge should not PUT, puts=%v", stub.puts)
	}
}

func TestDescribeAddTableConnectorDelta(t *testing.T) {
	before := map[string]string{"table.include.list": "public.a"}
	after := map[string]string{"table.include.list": "public.a,public.orders(_[0-9]+|[0-9]+)"}
	added, routers := describeAddTableConnectorDelta(before, after, []TableMapping{{
		IsShardGroup: true,
		TargetTable:  "orders",
		SourceTables: []string{"orders_0"},
	}})
	if len(added) != 1 || added[0] != "public.orders(_[0-9]+|[0-9]+)" {
		t.Fatalf("added=%v", added)
	}
	if len(routers) != 1 || routers[0] != "orders" {
		t.Fatalf("routers=%v", routers)
	}
}
