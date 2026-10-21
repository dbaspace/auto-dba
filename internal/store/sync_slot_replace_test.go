package store

import (
	"os"
	"testing"

	"github.com/google/uuid"
)

func TestReplaceHostSlotsUpdatesBySlotName(t *testing.T) {
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run")
	}
	d, err := Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	slotName := "test_slot_replace_" + uuid.NewString()
	otherName := "test_slot_other_" + uuid.NewString()
	keepName := "test_slot_keep_" + uuid.NewString()
	displayHost := "pg.example.test"
	oldHost := "10.0.0.1"
	dupHost := "10.0.0.2"
	otherInst := "other.example.test"
	now := "2020-01-01T00:00:00Z"
	oldID := uuid.NewString()
	dupID := uuid.NewString()
	dropID := uuid.NewString()
	keepID := uuid.NewString()

	t.Cleanup(func() {
		_, _ = d.Exec(`DELETE FROM tbl_sync_slots WHERE slot_name IN (?,?,?)`, slotName, otherName, keepName)
	})
	insert := `INSERT INTO tbl_sync_slots(id, host, port, slot_name, database_name, active, active_pid, restart_lsn, confirmed_flush_lsn, lag_bytes, plugin, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`
	rows := [][]any{
		{oldID, oldHost, 5432, slotName, "db_old", 1, int64(111), "0/1", "0/2", int64(1000), "pgoutput", now},
		{dupID, dupHost, 5432, slotName, "db_dup", 1, int64(1), "0/3", "0/4", int64(1), "pgoutput", now},
		{dropID, displayHost, 5432, otherName, "db_drop", 1, int64(2), "0/5", "0/6", int64(2), "pgoutput", now},
		{keepID, otherInst, 5432, keepName, "db_keep", 1, int64(3), "0/7", "0/8", int64(3), "pgoutput", now},
	}
	for _, r := range rows {
		if _, err := d.Exec(insert, r...); err != nil {
			t.Fatal(err)
		}
	}

	err = d.ReplaceHostSlots(displayHost, 5432, []string{oldHost}, []SyncSlot{{
		SlotName:          slotName,
		Database:          "db_new",
		Active:            true,
		ActivePID:         999,
		RestartLSN:        "0/AA",
		ConfirmedFlushLSN: "0/BB",
		LagBytes:          4096,
		Plugin:            "decoderbufs",
	}})
	if err != nil {
		t.Fatal(err)
	}

	hits, err := d.listSlotsByName(slotName)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("slot rows=%d want 1", len(hits))
	}
	got, err := d.GetSyncSlot(hits[0].ID)
	if err != nil || got == nil {
		t.Fatalf("get slot: %v", err)
	}
	if got.Host != displayHost || got.Port != 5432 || got.ActivePID != 999 || got.LagBytes != 4096 || got.RestartLSN != "0/AA" || got.ConfirmedFlushLSN != "0/BB" || got.Database != "db_new" {
		t.Fatalf("updated slot = %+v", got)
	}
	if got.ID != oldID && got.ID != dupID {
		t.Fatalf("kept id %s is neither old nor duplicate", got.ID)
	}

	if dropped, err := d.GetSyncSlot(dropID); err != nil || dropped != nil {
		t.Fatalf("host slot missing from collect should be deleted, got %+v err %v", dropped, err)
	}
	kept, err := d.GetSyncSlot(keepID)
	if err != nil || kept == nil || kept.SlotName != keepName || kept.ActivePID != 3 {
		t.Fatalf("other instance slot changed: %+v err %v", kept, err)
	}

	if err := d.UpsertSlotMeta(&SyncSlot{Host: "unused", Port: 1, SlotName: slotName, Database: "db_meta", Active: false, Plugin: "pgoutput"}); err != nil {
		t.Fatal(err)
	}
	after, err := d.GetSyncSlot(got.ID)
	if err != nil || after == nil {
		t.Fatal(err)
	}
	if after.ActivePID != 999 || after.LagBytes != 4096 || after.RestartLSN != "0/AA" || after.Database != "db_meta" {
		t.Fatalf("meta upsert overwrote collect fields: %+v", after)
	}
}
