package srcluster

import (
	"testing"

	"auto-dba/internal/store"
)

func TestCanMarkClusterReady(t *testing.T) {
	if err := canMarkClusterReady(store.SrClusterFailed, 0); err != nil {
		t.Fatal(err)
	}
	if err := canMarkClusterReady(store.SrClusterDraft, 2); err != nil {
		t.Fatal(err)
	}
	if err := canMarkClusterReady(store.SrClusterDraft, 0); err == nil {
		t.Fatal("draft without nodes should fail")
	}
	if err := canMarkClusterReady(store.SrClusterReady, 1); err == nil {
		t.Fatal("ready should fail")
	}
	if err := canMarkClusterReady(store.SrClusterIniting, 1); err == nil {
		t.Fatal("initializing should fail")
	}
}
