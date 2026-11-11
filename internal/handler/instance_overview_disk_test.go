package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestLatestCloudDiskBytes(t *testing.T) {
	const gb = int64(1024 * 1024 * 1024)
	got := latestCloudDiskBytes([]store.CloudInstance{
		{InstanceID: "bj", DiskGB: 1000, SyncedAt: "2026-11-01T00:00:00Z"},
		{InstanceID: "bj", DiskGB: 2000, SyncedAt: "2026-11-10T00:00:00Z"},
		{InstanceID: "sg", DiskGB: 550, SyncedAt: "2026-11-10T00:00:00Z"},
		{InstanceID: "sg", DiskGB: 100, SyncedAt: "2026-11-01T00:00:00Z"},
		{InstanceID: "", DiskGB: 999, SyncedAt: "2026-11-11T00:00:00Z"},
		{InstanceID: "zero", DiskGB: 0, SyncedAt: "2026-11-11T00:00:00Z"},
	})
	if got["bj"] != 2000*gb {
		t.Fatalf("bj=%d", got["bj"])
	}
	if got["sg"] != 550*gb {
		t.Fatalf("sg=%d", got["sg"])
	}
	if _, ok := got["zero"]; ok {
		t.Fatal("zero disk should be ignored")
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
}
