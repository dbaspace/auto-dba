package ha

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestSwitchoverChecklistByMode(t *testing.T) {
	cases := []struct {
		engine, mode, wantSub string
	}{
		{"mysql", "orchestrator", "Orchestrator"},
		{"mysql", "mha", "masterha_master_switch"},
		{"mysql", "mgr", "ONLINE"},
		{"mysql", "pxc", "wsrep"},
		{"postgres", "patroni", "Patroni"},
		{"postgres", "pg_autofailover", "pg_autoctl"},
		{"redis", "sentinel", "SENTINEL"},
	}
	for _, c := range cases {
		list := SwitchoverChecklist(c.engine, c.mode)
		if len(list) < 3 {
			t.Fatalf("%s/%s too short: %v", c.engine, c.mode, list)
		}
		joined := strings.Join(list, " ")
		if !strings.Contains(joined, c.wantSub) {
			t.Fatalf("%s/%s missing %q in %v", c.engine, c.mode, c.wantSub, list)
		}
	}
}

func TestValidHAModeByEngine(t *testing.T) {
	if !store.ValidHAMode("mysql", "mgr") || store.ValidHAMode("mysql", "patroni") {
		t.Fatal("mysql modes")
	}
	if !store.ValidHAMode("postgres", "patroni") || store.ValidHAMode("postgres", "mgr") {
		t.Fatal("postgres modes")
	}
	if !store.ValidHAMode("postgres", "pg_autofailover") {
		t.Fatal("postgres pg_autofailover")
	}
	if !store.CompatibleHAController("postgres", "pg_autofailover", "pg_autofailover") {
		t.Fatal("pg_autofailover compat")
	}
	if !store.ValidHAMode("redis", "sentinel") || store.ValidHAMode("redis", "orchestrator") {
		t.Fatal("redis modes")
	}
	if !store.CompatibleHAController("mysql", "orchestrator", "orchestrator") {
		t.Fatal("orchestrator compat")
	}
	if !store.CompatibleHAController("mysql", "mha", "mha") || store.CompatibleHAController("mysql", "mha", "orchestrator") {
		t.Fatal("mha compat")
	}
	if !store.HAAutoSwitchoverMode("mha") {
		t.Fatal("mha auto")
	}
	if store.CompatibleHAController("mysql", "mgr", "orchestrator") {
		t.Fatal("mgr should not auto-bind orchestrator")
	}
}
