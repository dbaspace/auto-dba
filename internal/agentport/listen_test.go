package agentport

import "testing"

func TestValidateListenPort(t *testing.T) {
	if err := ValidateListenPort(8091); err != nil {
		t.Fatalf("8091: %v", err)
	}
	if err := ValidateListenPort(9030); err == nil {
		t.Fatal("9030 should be blocked")
	}
	if err := ValidateListenPort(22); err == nil {
		t.Fatal("22 should be blocked")
	}
	if err := ValidateListenPort(80); err == nil {
		t.Fatal("80 should be blocked")
	}
}

func TestAllowFuserKill(t *testing.T) {
	if !AllowFuserKill(8090) || !AllowFuserKill(8091) || !AllowFuserKill(8092) {
		t.Fatal("agent band should allow fuser")
	}
	if AllowFuserKill(9030) || AllowFuserKill(18091) {
		t.Fatal("non-band / blocked should not allow fuser")
	}
}
