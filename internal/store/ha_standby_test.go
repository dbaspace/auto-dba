package store

import "testing"

func TestStandbyControllerIDsRoundTrip(t *testing.T) {
	raw := encodeStandbyControllerIDs([]string{" a ", "b", "a", ""})
	got := decodeStandbyControllerIDs(raw)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v", got)
	}
	norm := NormalizeStandbyControllerIDs("a", []string{"a", "b", "c"})
	if len(norm) != 2 || norm[0] != "b" || norm[1] != "c" {
		t.Fatalf("norm %#v", norm)
	}
}
