package srcluster

import (
	"testing"

	"auto-dba/internal/store"
)

func TestArtifactInstallReady(t *testing.T) {
	if artifactInstallReady(nil) {
		t.Fatal("nil")
	}
	if artifactInstallReady(&store.SrReleaseArtifact{ObjectKey: "a", SHA256: "abc"}) {
		t.Fatal("short sha")
	}
	sum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !artifactInstallReady(&store.SrReleaseArtifact{ObjectKey: "starrocks/x.fe.tar.gz", SHA256: sum}) {
		t.Fatal("valid")
	}
	if artifactInstallReady(&store.SrReleaseArtifact{ObjectKey: "", SHA256: sum}) {
		t.Fatal("empty key")
	}
}

func TestArtifactGapErrorIncompleteSHA(t *testing.T) {
	err := artifactGapError("3.2.10", "rid", "fe", &store.SrReleaseArtifact{
		ObjectKey: "starrocks/StarRocks-3.2.10.fe.tar.gz",
		SHA256:    "df5d56e8ae26b6e26b91e7925cf7048a49df90a08a744c3d6",
	})
	if err == nil || !containsAll(err.Error(), "不完整", "64") {
		t.Fatalf("got %v", err)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !containsStr(s, p) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
