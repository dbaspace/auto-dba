package store

import "testing"

func TestIsBuiltinRoleCode(t *testing.T) {
	for _, code := range []string{"admin", "operator", "viewer", "developer", "dba"} {
		if !IsBuiltinRoleCode(code) {
			t.Fatalf("expected builtin %s", code)
		}
	}
	if IsBuiltinRoleCode("pos_read") {
		t.Fatal("pos_read must not be builtin")
	}
	if IsBuiltinRoleCode("") {
		t.Fatal("empty must not be builtin")
	}
}
