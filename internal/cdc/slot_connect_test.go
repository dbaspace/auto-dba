package cdc

import (
	"testing"

	"auto-dba/internal/store"
)

func TestConnectSlotMetaPostgresOnly(t *testing.T) {
	cfg := map[string]any{
		"connector.class":   "io.debezium.connector.postgresql.PostgresConnector",
		"slot.name":         "slot_test_slot_ai_bd1383",
		"topic.prefix":      "test_db_ai",
		"database.dbname":   "db_ai",
		"database.hostname": "10.0.0.8",
		"database.port":     "5432",
	}
	meta, ok := connectSlotMeta("http://10.100.112.17:8085", "test_db_ai", cfg)
	if !ok || meta.SlotName != "slot_test_slot_ai_bd1383" || meta.Topic != "test_db_ai" || meta.ConnectorName != "test_db_ai" || meta.Port != 5432 {
		t.Fatalf("meta = %+v ok=%v", meta, ok)
	}
	if _, ok := connectSlotMeta("http://10.100.112.17:8085", "mysql_one", map[string]any{
		"connector.class": "io.debezium.connector.mysql.MySqlConnector",
		"slot.name":       "ignored",
	}); ok {
		t.Fatal("mysql connector should be skipped")
	}
}

func TestAssignConnectSlotMetaUniqueNameIgnoresHost(t *testing.T) {
	slots := []store.SyncSlot{{
		ID: "s1", Host: "pg.test.restosuite.ai", Port: 5432, SlotName: "slot_test_slot_ai_bd1383", Database: "db_ai",
	}}
	metas := []ConnectSlotMeta{{
		Base: "http://10.100.112.17:8085", ConnectorName: "test_db_ai", Topic: "test_db_ai",
		SlotName: "slot_test_slot_ai_bd1383", Database: "db_ai", Host: "10.0.0.8", Port: 5432,
	}}
	got := AssignConnectSlotMeta(slots, metas)
	if got["s1"].ConnectorName != "test_db_ai" || got["s1"].Topic != "test_db_ai" {
		t.Fatalf("assigned = %+v", got)
	}
}

func TestAssignConnectSlotMetaDisambiguatesByDatabase(t *testing.T) {
	slots := []store.SyncSlot{
		{ID: "a", Host: "pg.test.restosuite.ai", Port: 5432, SlotName: "shared_slot", Database: "db_ai"},
		{ID: "b", Host: "pg.test.restosuite.ai", Port: 5432, SlotName: "shared_slot", Database: "db_sales_vulcan"},
	}
	metas := []ConnectSlotMeta{
		{Base: "http://10.100.112.17:8085", ConnectorName: "test_db_ai", Topic: "test_db_ai", SlotName: "shared_slot", Database: "db_ai", Host: "pg.test.restosuite.ai", Port: 5432},
		{Base: "http://10.100.112.17:8085", ConnectorName: "dev_db_ai", Topic: "dev_db_ai", SlotName: "shared_slot", Database: "db_sales_vulcan", Host: "pg.test.restosuite.ai", Port: 5432},
	}
	got := AssignConnectSlotMeta(slots, metas)
	if got["a"].ConnectorName != "test_db_ai" || got["b"].ConnectorName != "dev_db_ai" {
		t.Fatalf("assigned = %+v", got)
	}
}

func TestAssignConnectSlotMetaSkipsAmbiguous(t *testing.T) {
	slots := []store.SyncSlot{
		{ID: "a", Host: "pg.test.restosuite.ai", Port: 5432, SlotName: "shared_slot", Database: "db_ai"},
		{ID: "b", Host: "10.0.0.9", Port: 5432, SlotName: "shared_slot", Database: "db_ai"},
	}
	metas := []ConnectSlotMeta{
		{Base: "http://10.100.112.17:8085", ConnectorName: "one", SlotName: "shared_slot", Database: "db_ai", Host: "10.1.1.1", Port: 5432},
		{Base: "http://10.100.112.17:8085", ConnectorName: "two", SlotName: "shared_slot", Database: "db_ai", Host: "10.2.2.2", Port: 5432},
	}
	if got := AssignConnectSlotMeta(slots, metas); len(got) != 0 {
		t.Fatalf("ambiguous should not assign: %+v", got)
	}
}

func TestFillSlotConnectFallbackKeepsTaskMatch(t *testing.T) {
	rows := FillSlotConnectFallback([]SlotRow{{
		SyncSlot: store.SyncSlot{ConnectMetaBase: "http://10.100.112.17:8085", ConnectMetaTask: "from-connect", ConnectMetaTopic: "from-connect"},
		TaskID:   "t1", TaskName: "平台任务", ConnectBase: "http://10.128.20.112:8083", Topic: "platform", Connector: "10.128.20.112:8083",
	}})
	if rows[0].TaskName != "平台任务" || rows[0].Topic != "platform" || rows[0].Connector != "10.128.20.112:8083" {
		t.Fatalf("task match overwritten: %+v", rows[0])
	}
	empty := FillSlotConnectFallback([]SlotRow{{
		SyncSlot: store.SyncSlot{
			ConnectMetaBase:  "http://10.100.112.17:8085",
			ConnectMetaName:  "test_db_ai",
			ConnectMetaTask:  "test_db_ai",
			ConnectMetaTopic: "test_db_ai",
		},
	}})
	if empty[0].TaskName != "test_db_ai" || empty[0].Topic != "test_db_ai" || empty[0].Connector != "10.100.112.17:8085" {
		t.Fatalf("fallback = %+v", empty[0])
	}
}
