package store

import "testing"

func TestNormalizeClientScopes(t *testing.T) {
	if got := NormalizeClientScopes(""); got != ClientScopeQuery {
		t.Fatalf("empty -> %q", got)
	}
	if got := NormalizeClientScopes("query,credential,query"); got != ClientScopeQuery {
		t.Fatalf("strip credential -> %q", got)
	}
	if !ScopeHas("query", "query") || ScopeHas("query", "credential") {
		t.Fatal("ScopeHas mismatch")
	}
}

func TestHashClientPATStable(t *testing.T) {
	a := HashClientPAT("adba_pat_abc")
	b := HashClientPAT("adba_pat_abc")
	if a != b || a == "" {
		t.Fatal("hash unstable")
	}
	raw, prefix, err := GenerateClientPATRaw()
	if err != nil || raw == "" || prefix == "" || !hasPrefix(raw, PATPrefix) {
		t.Fatalf("generate: raw=%q prefix=%q err=%v", raw, prefix, err)
	}
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
