package cdc

import (
	"fmt"
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestAttachSlotCDC(t *testing.T) {
	inst := store.Instance{ID: "i1", Host: "10.0.0.8", DomainName: "pg.example", Port: 5432}
	tasks := []store.CDCTask{{
		ID: "t1", Name: "订单同步", SourceID: "i1", SourceDB: "orders",
		Description: `-- auto-dba-cdc:{"slot_name":"dbz_orders","connector_name":"conn-orders","connect_base_url":"http://10.1.2.3:8083","topic_prefix":"orders"}`,
	}}
	rows := AttachSlotCDC([]store.SyncSlot{
		{Host: "pg.example", Port: 5432, SlotName: "dbz_orders", Database: "orders"},
		{Host: "10.0.0.8", Port: 5432, SlotName: "other_slot", Database: "orders"},
		{Host: "10.9.9.9", Port: 5432, SlotName: "dbz_orders", Database: "orders"},
	}, tasks, []store.Instance{inst})
	if rows[0].TaskName != "订单同步" || rows[0].Connector != "10.1.2.3:8083" || rows[0].Topic != "orders" || rows[0].ConnectorName != "conn-orders" {
		t.Fatalf("matched row = %+v", rows[0])
	}
	if rows[1].TaskName != "" || rows[2].TaskName != "" {
		t.Fatalf("unmatched rows should stay empty: %+v %+v", rows[1], rows[2])
	}
}

func TestAttachSlotCDCShardBinding(t *testing.T) {
	inst := store.Instance{ID: "i1", Host: "10.0.0.8", Port: 5432}
	tasks := []store.CDCTask{{
		ID: "t1", Name: "分库", SourceID: "i1", SourceDB: "db_0",
		Description: `-- auto-dba-cdc:{"slot_name":"slot_rep","connector_name":"conn-rep","topic_prefix":"shard","shard_db_bindings":[{"db_name":"db_1","slot_name":"slot_1","connector_name":"conn-1"}]}`,
	}}
	rows := AttachSlotCDC([]store.SyncSlot{
		{Host: "10.0.0.8", Port: 5432, SlotName: "slot_1", Database: "db_1"},
	}, tasks, []store.Instance{inst})
	if rows[0].ConnectorName != "conn-1" || rows[0].TaskName != "分库" || rows[0].Topic != "shard" {
		t.Fatalf("shard row = %+v", rows[0])
	}
}

func TestAttachSlotCDCIndexedManyTasks(t *testing.T) {
	inst := store.Instance{ID: "i1", Host: "10.0.0.8", Port: 5432}
	tasks := make([]store.CDCTask, 0, 50)
	for i := 0; i < 49; i++ {
		tasks = append(tasks, store.CDCTask{
			ID: fmt.Sprintf("noise-%d", i), Name: "noise", SourceID: "i1", SourceDB: "db",
			Description: fmt.Sprintf(`-- auto-dba-cdc:{"slot_name":"slot_%d","connector_name":"c%d","topic_prefix":"t"}`, i, i),
		})
	}
	tasks = append(tasks, store.CDCTask{
		ID: "hit", Name: "目标", SourceID: "i1", SourceDB: "orders",
		Description: `-- auto-dba-cdc:{"slot_name":"dbz_orders","connector_name":"conn-orders","connect_base_url":"http://10.1.2.3:8083","topic_prefix":"orders"}`,
	})
	rows := AttachSlotCDC([]store.SyncSlot{
		{Host: "10.0.0.8", Port: 5432, SlotName: "dbz_orders", Database: "orders"},
	}, tasks, []store.Instance{inst})
	if rows[0].TaskName != "目标" || rows[0].ConnectorName != "conn-orders" {
		t.Fatalf("indexed match = %+v", rows[0])
	}
}

func TestAttachSlotCDCPrefersInitOverAdd(t *testing.T) {
	inst := store.Instance{ID: "i1", Host: "pg.member04.bj.restosuite.cn", Port: 5432}
	tasks := []store.CDCTask{
		{
			ID: "add", Name: "rt_bj_db_member_004-add", TaskType: "add_table", SourceID: "i1", SourceDB: "db_marketing_004",
			Description: `-- auto-dba-cdc:{"slot_name":"rt_bj_slot_db_member_004","connector_name":"rt_bj_db_marketing_004","connect_base_url":"http://10.128.20.112:8083","shard_db_bindings":[{"db_name":"db_member_004","slot_name":"rt_bj_slot_db_member_004"}]}`,
		},
		{
			ID: "init", Name: "rt_bj_db_member_004", TaskType: "init", SourceID: "i1", SourceDB: "db_member_004",
			Description: `-- auto-dba-cdc:{"slot_name":"rt_bj_slot_db_member_004","connector_name":"rt_bj_db_member_004","connect_base_url":"http://10.128.20.112:8083"}`,
		},
	}
	rows := AttachSlotCDC([]store.SyncSlot{{
		Host: inst.Host, Port: 5432, SlotName: "rt_bj_slot_db_member_004", Database: "db_member_004",
	}}, tasks, []store.Instance{inst})
	if rows[0].TaskName != "rt_bj_db_member_004" || rows[0].ConnectorName != "rt_bj_db_member_004" {
		t.Fatalf("init should own the member slot, got %+v", rows[0])
	}
}

func TestAttachSlotCDCRejectsRepresentativeConnector(t *testing.T) {
	inst := store.Instance{ID: "i1", Host: "pg.member04.bj.restosuite.cn", Port: 5432}
	tasks := []store.CDCTask{{
		ID: "add", Name: "rt_bj_db_member_004-add", TaskType: "add_table", SourceID: "i1", SourceDB: "db_marketing_004",
		Description: `-- auto-dba-cdc:{"slot_name":"rt_bj_slot_db_member_004","connector_name":"rt_bj_db_marketing_004","connect_base_url":"http://10.128.20.112:8083","shard_db_bindings":[{"db_name":"db_member_004","slot_name":"rt_bj_slot_db_member_004"}]}`,
	}}
	rows := AttachSlotCDC([]store.SyncSlot{{
		Host: inst.Host, Port: 5432, SlotName: "rt_bj_slot_db_member_004", Database: "db_member_004",
	}}, tasks, []store.Instance{inst})
	if rows[0].ConnectorName != "" || rows[0].TaskName != "" {
		t.Fatalf("add task must not restart the marketing connector, got %+v", rows[0])
	}
}

func TestSlotQueryDatabases(t *testing.T) {
	got := slotQueryDatabases([]string{"db_ai", "postgres", " db_ai ", "orders"})
	if len(got) != 3 || got[0] != "postgres" || got[1] != "db_ai" || got[2] != "orders" {
		t.Fatalf("databases = %#v", got)
	}
}

func TestReplicationSlotsSQLIsCatalogOnly(t *testing.T) {
	low := strings.ToLower(replicationSlotsSQL)
	for _, bad := range []string{"pg_stat_activity", "pg_stat_replication", "pg_stat_wal"} {
		if strings.Contains(low, bad) {
			t.Fatalf("query touches %s", bad)
		}
	}
	if strings.Count(low, "pg_current_wal_lsn") != 1 {
		t.Fatalf("current lsn should be read once: %s", replicationSlotsSQL)
	}
	if !strings.Contains(low, "slot_type <> 'physical'") {
		t.Fatalf("logical slots only: %s", replicationSlotsSQL)
	}
	if !strings.Contains(low, "pg_wal_lsn_diff(cur.lsn, s.restart_lsn)") {
		t.Fatalf("lag should use restart_lsn only: %s", replicationSlotsSQL)
	}
	if strings.Contains(low, "coalesce(s.restart_lsn, s.confirmed_flush_lsn)") || strings.Contains(low, "coalesce(s.confirmed_flush_lsn, s.restart_lsn)") {
		t.Fatalf("lag must not fall back to confirmed_flush_lsn: %s", replicationSlotsSQL)
	}
	fallback := strings.ToLower(replicationSlotsSQLFallback)
	if !strings.Contains(fallback, "slot_type <> 'physical'") || !strings.Contains(fallback, "s.restart_lsn") {
		t.Fatalf("fallback = %s", replicationSlotsSQLFallback)
	}
}

func TestSlotDisplayHost(t *testing.T) {
	if got := SlotDisplayHost(&store.Instance{Host: "10.0.0.1,10.0.0.2", DomainName: "pg.example"}); got != "10.0.0.1" {
		t.Fatalf("host = %s", got)
	}
	if got := SlotDisplayHost(&store.Instance{DomainName: "pg.example"}); got != "pg.example" {
		t.Fatalf("domain = %s", got)
	}
}
