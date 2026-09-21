package dbmig

import "testing"

func TestMatchWild(t *testing.T) {
	if !matchWild("tmp_*", "tmp_a") {
		t.Fatal("expected match")
	}
	if matchWild("tmp_*", "data") {
		t.Fatal("unexpected match")
	}
	if !matchExclude([]string{"tmp_*"}, "db1", "tmp_x") {
		t.Fatal("exclude full")
	}
}
