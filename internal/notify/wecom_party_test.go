package notify

import "testing"

func TestParseWecomTestTarget(t *testing.T) {
	u, p := ParseWecomTestTarget("ccj")
	if u != "ccj" || p != "" {
		t.Fatalf("userid: %q %q", u, p)
	}
	u, p = ParseWecomTestTarget("15")
	if u != "" || p != "15" {
		t.Fatalf("party digit: %q %q", u, p)
	}
	u, p = ParseWecomTestTarget("party:15")
	if u != "" || p != "15" {
		t.Fatalf("party prefix: %q %q", u, p)
	}
	u, p = ParseWecomTestTarget("15|16")
	if u != "" || p != "15|16" {
		t.Fatalf("multi party: %q %q", u, p)
	}
}
