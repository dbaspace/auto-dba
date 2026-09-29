package srcluster

import "testing"

func TestRejectLockedDataDirKeys(t *testing.T) {
	ok := "# comment\nhttp_port = 8030\n"
	if err := RejectLockedDataDirKeys(ok); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	bad := "meta_dir = /data/meta\n"
	if err := RejectLockedDataDirKeys(bad); err == nil {
		t.Fatal("expected reject meta_dir")
	}
	bad2 := "  STORAGE_ROOT_PATH=/data\n"
	if err := RejectLockedDataDirKeys(bad2); err == nil {
		t.Fatal("expected reject storage_root_path")
	}
}
