package sqlexec

import "testing"

func TestDeployPayloadEscalate(t *testing.T) {
	if got := DeployPayloadEscalate(`{"env_codes":["test"],"escalate":"sudo"}`); got != "sudo_su" {
		t.Fatalf("got %q", got)
	}
	if got := DeployPayloadEscalate(`{"name":"x"}`); got != "" {
		t.Fatalf("empty escalate got %q", got)
	}
	if got := DeployPayloadEscalate(""); got != "" {
		t.Fatalf("blank got %q", got)
	}
}
