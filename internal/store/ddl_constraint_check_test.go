package store

import "testing"

func TestParseDDLConstraintCheckEnabled(t *testing.T) {
	for _, raw := range []string{"", "true", "TRUE", "1", "yes", "on", "maybe", "  "} {
		if !ParseDDLConstraintCheckEnabled(raw) {
			t.Fatalf("%q should keep the check enabled", raw)
		}
	}
	for _, raw := range []string{"false", "FALSE", "0", "no", "off", " OFF "} {
		if ParseDDLConstraintCheckEnabled(raw) {
			t.Fatalf("%q should turn the check off", raw)
		}
	}
}
