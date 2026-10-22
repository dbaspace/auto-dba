package dns

import "testing"

func TestCIDROverlap(t *testing.T) {
	over, err := CIDRsOverlap("10.0.0.0/8", "10.1.0.0/16")
	if err != nil || !over {
		t.Fatalf("contain got %v %v", over, err)
	}
	over, err = CIDRsOverlap("10.1.0.0/16", "10.2.0.0/16")
	if err != nil || over {
		t.Fatalf("disjoint got %v %v", over, err)
	}
	if err := CheckCIDRList([]string{"10.0.0.0/8"}, []string{"10.1.0.0/16"}); err == nil {
		t.Fatal("expected overlap reject")
	}
	if err := CheckCIDRList([]string{"192.168.1.0/24"}, []string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
}

func TestCorefileNoFallthrough(t *testing.T) {
	body, err := RenderCorefile(DeployOptions{
		Listen: "0.0.0.0:53", Upstream: "223.5.5.5:53", DBHost: "10.0.0.8", DBPort: 5432,
	}, "auto_dba_dns", "auto_dba_dns", "secret", "dns-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"check_intervial 5s", "refresh 20m", "forward . 223.5.5.5:53", "bind 0.0.0.0"} {
		if !contains(body, want) {
			t.Fatalf("missing %s\n%s", want, body)
		}
	}
	if contains(body, "fallthrough") || contains(body, "parsing_range_ids") {
		t.Fatalf("unexpected directive\n%s", body)
	}
}

func TestMX(t *testing.T) {
	s, err := EncodeMX("mail.example.com", 10)
	if err != nil || s != "mail.example.com|10" {
		t.Fatalf("%s %v", s, err)
	}
	host, pref, ok := DecodeMX(s)
	if !ok || host != "mail.example.com" || pref != 10 {
		t.Fatalf("%s %d %v", host, pref, ok)
	}
}

func TestOrigin(t *testing.T) {
	got, err := NormalizeOrigin("Restosuite.CN")
	if err != nil || got != "restosuite.cn." {
		t.Fatalf("%s %v", got, err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}
