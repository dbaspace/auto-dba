package cdc

import (
	"strings"
	"testing"
)

func TestPlanPublicationSlotSQL_initCreatesThenAdds(t *testing.T) {
	fresh, err := planPublicationSlotSQL(false, false, false, "rt_test_db", "slot_test_db_abc123", "public", []string{"orders"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(fresh.Statements, "\n")
	if !strings.Contains(joined, "CREATE PUBLICATION") || !strings.Contains(joined, "pg_create_logical_replication_slot") {
		t.Fatalf("init fresh=%s", joined)
	}
	if strings.Contains(joined, "ALTER PUBLICATION") {
		t.Fatalf("fresh init should not ADD: %s", joined)
	}
	if len(fresh.ReplicaIdentity) != 1 || fresh.ReplicaIdentity[0] != "orders" {
		t.Fatalf("full=%v", fresh.ReplicaIdentity)
	}

	existed, err := planPublicationSlotSQL(false, true, false, "rt_test_db", "slot_test_db_abc123", "public", []string{"orders", "items"}, []string{"items"})
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(existed.Statements, "\n")
	if strings.Contains(joined, "CREATE PUBLICATION") {
		t.Fatalf("existing pub must not CREATE: %s", joined)
	}
	if !strings.Contains(joined, "pg_create_logical_replication_slot") || !strings.Contains(joined, "ALTER PUBLICATION") {
		t.Fatalf("existed=%s", joined)
	}
	if len(existed.ReplicaIdentity) != 1 || existed.ReplicaIdentity[0] != "items" {
		t.Fatalf("full=%v", existed.ReplicaIdentity)
	}

	if _, err := planPublicationSlotSQL(false, true, true, "rt_test_db", "slot_x", "public", []string{"orders"}, nil); err == nil || !strings.Contains(err.Error(), "FOR ALL TABLES") {
		t.Fatalf("all tables err=%v", err)
	}
}

func TestPlanPublicationSlotSQL_addTableOnlyAlter(t *testing.T) {
	plan, err := planPublicationSlotSQL(true, true, false, "rt_test_db", "slot_test_db_abc123", "public", []string{"orders"}, []string{"orders"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Statements, "\n")
	if !strings.Contains(joined, "ALTER PUBLICATION") {
		t.Fatalf("add=%s", joined)
	}
	if strings.Contains(joined, "CREATE PUBLICATION") || strings.Contains(joined, "pg_create_logical_replication_slot") {
		t.Fatalf("add table must not create pub/slot: %s", joined)
	}
}
