package clientaccess

import "testing"

func TestNormalizeSourceCIDRs(t *testing.T) {
	got, err := NormalizeSourceCIDRs("61.49.49.50, 10.0.0.0/8\n192.168.1.10")
	if err != nil {
		t.Fatal(err)
	}
	if got != "61.49.49.50\n10.0.0.0/8\n192.168.1.10" {
		t.Fatalf("got %q", got)
	}
	if _, err := NormalizeSourceCIDRs("  "); err == nil {
		t.Fatal("empty should fail")
	}
	if _, err := NormalizeSourceCIDRs("not-an-ip"); err == nil {
		t.Fatal("invalid should fail")
	}
}

func TestSourceAllows(t *testing.T) {
	raw := "61.49.49.50\n10.0.0.0/8"
	if !SourceAllows(raw, "61.49.49.50") {
		t.Fatal("public ip")
	}
	if !SourceAllows(raw, "10.8.0.12") {
		t.Fatal("cidr")
	}
	if SourceAllows(raw, "192.168.1.1") {
		t.Fatal("outside")
	}
	if SourceAllows("", "10.0.0.1") {
		t.Fatal("empty list denies")
	}
	if !SourceAllows("10.1.1.1", "::ffff:10.1.1.1") {
		t.Fatal("v4-mapped")
	}
}

func TestObservedClientIP(t *testing.T) {
	trusted := ParseTrustedProxies("10.0.0.8/32")
	if got := ObservedClientIP("203.0.113.9:443", "61.49.49.50, 10.0.0.8", nil); got != "203.0.113.9" {
		t.Fatalf("ignore xff when untrusted, got %s", got)
	}
	if got := ObservedClientIP("10.0.0.8:443", "61.49.49.50, 10.0.0.8", trusted); got != "61.49.49.50" {
		t.Fatalf("left-most xff from trusted proxy, got %s", got)
	}
	if got := ObservedClientIP("198.51.100.4:443", "61.49.49.50", trusted); got != "198.51.100.4" {
		t.Fatalf("untrusted peer ignores xff, got %s", got)
	}
}
