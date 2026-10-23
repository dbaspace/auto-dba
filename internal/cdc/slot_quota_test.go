package cdc

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatCreateReplicationSlotErrorQuota(t *testing.T) {
	err := FormatCreateReplicationSlotError(nil, nil, errors.New("all replication slots are in use"))
	if err == nil || !strings.Contains(err.Error(), "逻辑复制槽已满") || !strings.Contains(err.Error(), "max_replication_slots") {
		t.Fatal(err)
	}
}

func TestReplicationSlotQuotaAlmostFull(t *testing.T) {
	if !ReplicationSlotQuotaAlmostFull(15, 16) || ReplicationSlotQuotaAlmostFull(14, 16) {
		t.Fatal("almost full")
	}
	if !ReplicationSlotQuotaFull(16, 16) || ReplicationSlotQuotaFull(15, 16) {
		t.Fatal("full")
	}
}

func TestSlotCreateBlocked(t *testing.T) {
	if !slotCreateBlocked(errors.New("canceling statement due to statement timeout")) {
		t.Fatal("expected blocked")
	}
	if slotCreateBlocked(errors.New("already exists")) {
		t.Fatal("exists is not a blocker")
	}
}
