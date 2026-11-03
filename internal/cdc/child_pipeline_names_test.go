package cdc

import "testing"

func TestChildPipelineNamesPrefersParent(t *testing.T) {
	extra := map[string]string{
		"publication_name": "ai_beijing_db_bill_7",
		"slot_name":        "slot_ai_beijing_db_bill_7",
		"connector_name":   "ai_beijing_db_bill_7",
		"topic_prefix":     "ai_beijing_db_bill_7",
	}
	parent := map[string]string{
		"publication_name": "ai_beijing_db_bill_29",
		"slot_name":        "slot_ai_beijing_db_bill_29",
		"connector_name":   "ai_beijing_db_bill_29",
		"topic_prefix":     "ai_beijing_db_bill_29",
	}
	pub, slot, connector, topic := childPipelineNames(extra, parent)
	if pub != "ai_beijing_db_bill_29" || slot != "slot_ai_beijing_db_bill_29" ||
		connector != "ai_beijing_db_bill_29" || topic != "ai_beijing_db_bill_29" {
		t.Fatalf("got pub=%s slot=%s connector=%s topic=%s", pub, slot, connector, topic)
	}
}

func TestChildPipelineNamesFallsBackToChild(t *testing.T) {
	extra := map[string]string{
		"publication_name": "child_pub",
		"slot_name":        "child_slot",
		"connector_name":   "child_conn",
		"topic_prefix":     "child_topic",
	}
	pub, slot, connector, topic := childPipelineNames(extra, map[string]string{})
	if pub != "child_pub" || slot != "child_slot" || connector != "child_conn" || topic != "child_topic" {
		t.Fatalf("got pub=%s slot=%s connector=%s topic=%s", pub, slot, connector, topic)
	}
}

func TestInheritParentCDCNamesOverwritesPipelineIdentity(t *testing.T) {
	extra := map[string]string{
		"publication_name": "ai_beijing_db_bill_7",
		"slot_name":        "slot_ai_beijing_db_bill_7",
		"connector_name":   "ai_beijing_db_bill_7",
		"topic_prefix":     "ai_beijing_db_bill_7",
		"source_schema":    "billing",
	}
	parent := map[string]string{
		"publication_name": "ai_beijing_db_bill_29",
		"slot_name":        "slot_ai_beijing_db_bill_29",
		"connector_name":   "ai_beijing_db_bill_29",
		"topic_prefix":     "ai_beijing_db_bill_29",
		"source_schema":    "public",
		"debezium_mode":    "after_only",
	}
	inheritParentCDCNames(extra, parent)
	if extra["publication_name"] != "ai_beijing_db_bill_29" {
		t.Fatalf("publication=%s", extra["publication_name"])
	}
	if extra["connector_name"] != "ai_beijing_db_bill_29" {
		t.Fatalf("connector=%s", extra["connector_name"])
	}
	// 非空的 source_schema 不覆盖
	if extra["source_schema"] != "billing" {
		t.Fatalf("source_schema=%s", extra["source_schema"])
	}
	if extra["debezium_mode"] != "after_only" {
		t.Fatalf("debezium_mode=%s", extra["debezium_mode"])
	}
}

func TestAddTableBindingsUseParentPipelineForWrongChildExtra(t *testing.T) {
	// 复现：子任务 extra 带 _7，父任务是 db_bill_29；bindings 须用 _29。
	parentExtra := map[string]string{
		"publication_name": "ai_beijing_db_bill_29",
		"slot_name":        "slot_ai_beijing_db_bill_29",
		"connector_name":   "ai_beijing_db_bill_29",
		"env":              "beijing",
	}
	childExtra := map[string]string{
		"publication_name": "ai_beijing_db_bill_7",
		"slot_name":        "slot_ai_beijing_db_bill_7",
		"connector_name":   "ai_beijing_db_bill_7",
	}
	pub, slot, connector, _ := childPipelineNames(childExtra, parentExtra)
	bindings := ResolveOrBuildShardBindings(parentExtra, "db_bill_29", pub, slot, connector)
	if len(bindings) != 1 {
		t.Fatalf("bindings=%v", bindings)
	}
	b := bindings[0]
	if b.DbName != "db_bill_29" {
		t.Fatalf("db=%s", b.DbName)
	}
	if b.PublicationName != "ai_beijing_db_bill_29" {
		t.Fatalf("publication=%s", b.PublicationName)
	}
	if b.ConnectorName != "ai_beijing_db_bill_29" {
		t.Fatalf("connector=%s", b.ConnectorName)
	}
	if b.SlotName != "slot_ai_beijing_db_bill_29" {
		t.Fatalf("slot=%s", b.SlotName)
	}
}
