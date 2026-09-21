package cdc

import (
	"testing"
)

func TestMissingFromTableSet(t *testing.T) {
	have := map[string]struct{}{
		"orders": {},
		"users":  {},
	}
	got := MissingFromTableSet(have, "public", []string{"orders", "missing_a", "Users", "missing_b", "missing_a"})
	if len(got) != 2 {
		t.Fatalf("got %v want 2 missing", got)
	}
	if got[0] != "public.missing_a" || got[1] != "public.missing_b" {
		t.Fatalf("got %v", got)
	}
}

func TestMissingFromTableSetEmpty(t *testing.T) {
	if got := MissingFromTableSet(nil, "public", nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if got := MissingFromTableSet(map[string]struct{}{}, "public", []string{"a"}); len(got) != 1 || got[0] != "public.a" {
		t.Fatalf("got %v", got)
	}
}
