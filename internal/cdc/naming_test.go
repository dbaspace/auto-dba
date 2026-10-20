package cdc

import (
	"strings"
	"testing"
)

func TestRoutineLoadNameFor_prefixRl(t *testing.T) {
	if got := GenerateRoutineLoadName("test", "order_db", "tbl_order"); got != "rl_test_order_db_tbl_order" {
		t.Fatalf("GenerateRoutineLoadName: %s", got)
	}
	// 自定义 topic_prefix 与默认不同 → rl_{topic_prefix}_{target}
	if got := RoutineLoadNameFor("test", "order_db", "rt_test_db_order_master", "tbl_order"); got != "rl_rt_test_db_order_master_tbl_order" {
		t.Fatalf("custom prefix: %s", got)
	}
	// 默认前缀与 {env}_{db} 相同 → 与 GenerateRoutineLoadName 一致
	if got := RoutineLoadNameFor("test", "order_db", "test_order_db", "tbl_order"); got != "rl_test_order_db_tbl_order" {
		t.Fatalf("default prefix: %s", got)
	}
}

func TestResolveInitNaming_defaults(t *testing.T) {
	n := ResolveInitNaming(InitNamingInput{Env: "test", SourceDB: "db_ai"})
	if n.PublicationName != "test_db_ai" || n.ConnectorName != "test_db_ai" || n.TopicPrefix != "test_db_ai" {
		t.Fatalf("naming=%+v", n)
	}
	if !strings.HasPrefix(n.SlotName, "slot_test_db_ai_") {
		t.Fatalf("slot=%s", n.SlotName)
	}
	suffix := strings.TrimPrefix(n.SlotName, "slot_test_db_ai_")
	if len(suffix) != 6 {
		t.Fatalf("slot suffix want 6 hex, got %q", suffix)
	}
}

func TestResolveInitNaming_publicationCascade(t *testing.T) {
	n := ResolveInitNaming(InitNamingInput{
		Env:             "test",
		SourceDB:        "db_dba_000",
		PublicationName: "test_db_dba",
	})
	if n.PublicationName != "test_db_dba" || n.ConnectorName != "test_db_dba" || n.TopicPrefix != "test_db_dba" {
		t.Fatalf("cascade=%+v", n)
	}
	if !strings.HasPrefix(n.SlotName, "slot_test_db_dba_") {
		t.Fatalf("slot=%s", n.SlotName)
	}
}

func TestResolveInitNaming_explicitSlotKept(t *testing.T) {
	n := ResolveInitNaming(InitNamingInput{
		Env:      "test",
		SourceDB: "db_ai",
		SlotName: "slot_custom",
	})
	if n.SlotName != "slot_custom" {
		t.Fatalf("slot=%s", n.SlotName)
	}
	if n.PublicationName != "test_db_ai" {
		t.Fatalf("pub=%s", n.PublicationName)
	}
}
