package store

import "testing"

func TestIsMaskedSecret(t *testing.T) {
	if !isMaskedSecret("********") {
		t.Fatal("stars should be masked")
	}
	if isMaskedSecret("AKID123") {
		t.Fatal("real key must not be masked")
	}
	if !isMaskedSecret("") {
		t.Fatal("empty treated as keep-current")
	}
}

func TestClampInt(t *testing.T) {
	if got := clampInt(0, 1, 10, 3); got != 3 {
		t.Fatalf("zero -> def, got %d", got)
	}
	if got := clampInt(100, 1, 10, 3); got != 10 {
		t.Fatalf("over max, got %d", got)
	}
	if got := clampInt(-1, 0, 10, 0); got != 0 {
		t.Fatalf("under min, got %d", got)
	}
}
