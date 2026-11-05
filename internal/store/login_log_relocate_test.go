package store

import "testing"

func TestRelocatePrivateLoginIP(t *testing.T) {
	ip, lan, changed := relocatePrivateLoginIP("10.10.102.163", "")
	if ip != "" || lan != "10.10.102.163" || !changed {
		t.Fatalf("empty lan got ip=%q lan=%q changed=%v", ip, lan, changed)
	}
	ip, lan, changed = relocatePrivateLoginIP("10.10.102.163", "10.10.102.163")
	if ip != "" || lan != "10.10.102.163" || !changed {
		t.Fatalf("same lan got ip=%q lan=%q changed=%v", ip, lan, changed)
	}
	ip, lan, changed = relocatePrivateLoginIP("61.49.49.50", "")
	if ip != "61.49.49.50" || lan != "" || changed {
		t.Fatalf("public got ip=%q lan=%q changed=%v", ip, lan, changed)
	}
	ip, lan, changed = relocatePrivateLoginIP("fc00::1", "")
	if ip != "" || lan != "fc00::1" || !changed {
		t.Fatalf("ula got ip=%q lan=%q changed=%v", ip, lan, changed)
	}
}
