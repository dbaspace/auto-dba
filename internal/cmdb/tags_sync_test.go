package cmdb

import "testing"

func TestSyncMonitorTagsRequiresTenant(t *testing.T) {
	if err := (&Service{}).SyncMonitorTagsIntoCmdb("  "); err == nil {
		t.Fatal("expected tenant error")
	}
}
