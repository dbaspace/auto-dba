package handler

import (
	"testing"

	"auto-dba/internal/instoverview"
	"auto-dba/internal/store"
)

func TestAssignCloudDisk(t *testing.T) {
	const gb = int64(1024 * 1024 * 1024)
	rows := []store.CloudInstance{
		{InstanceID: "bj", CloudID: "postgres-old", DiskGB: 1000, SyncedAt: "2026-11-01T00:00:00Z"},
		{InstanceID: "bj", CloudID: "postgres-bj", DiskGB: 2000, SyncedAt: "2026-11-10T00:00:00Z"},
		{InstanceID: "other-tenant", CloudID: "postgres-sg", DiskGB: 550, SyncedAt: "2026-11-10T00:00:00Z"},
		{InstanceID: "sg", CloudID: "postgres-stale", DiskGB: 100, SyncedAt: "2026-11-01T00:00:00Z"},
		{InstanceID: "zero", DiskGB: 0, SyncedAt: "2026-11-11T00:00:00Z"},
	}
	got := assignCloudDisk([]platformDiskKey{
		{ID: "bj", CloudID: "postgres-bj"},
		{ID: "sg", CloudID: "postgres-sg"},
		{ID: "zero"},
		{ID: "plain"},
	}, rows)
	if got["bj"] != 2000*gb {
		t.Fatalf("bj=%d", got["bj"])
	}
	if got["sg"] != 550*gb {
		t.Fatalf("sg=%d want cloud id match across tenant", got["sg"])
	}
	if _, ok := got["zero"]; ok {
		t.Fatal("zero disk should be ignored")
	}
	if _, ok := got["plain"]; ok {
		t.Fatal("unlinked instance should have no disk")
	}
}

func TestApplyCloudCapacityRate(t *testing.T) {
	const gb = int64(1024 * 1024 * 1024)
	s := &instoverview.Sample{DataBytes: 1003 * gb, DataKnown: true, StorageRate: 24, StorageRateKnown: true}
	applyCloudCapacity(s, 2000*gb)
	if !s.DiskKnown || s.DiskBytes != 2000*gb {
		t.Fatalf("disk=%d known=%v", s.DiskBytes, s.DiskKnown)
	}
	if !s.StorageRateKnown || s.StorageRate < 50.1 || s.StorageRate > 50.2 {
		t.Fatalf("rate=%v", s.StorageRate)
	}
}
