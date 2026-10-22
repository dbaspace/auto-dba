package handler

import (
	"testing"

	"auto-dba/internal/dns"
)

func TestEvalNodeAlerts(t *testing.T) {
	nodes := []dns.Node{
		{IP: "10.0.0.1", Name: "dns-1", Online: true, Synced: true},
		{IP: "10.0.0.2", Online: false, Synced: false},
		{IP: "10.0.0.3", Online: true, Synced: false},
		{IP: ""},
	}
	firing := map[string]bool{}
	for _, ck := range EvalNodeAlerts(nodes, 1000) {
		if ck.Firing {
			firing["dns:"+ck.Kind+":"+ck.IP] = true
		}
	}
	want := map[string]bool{"dns:offline:10.0.0.2": true, "dns:unsynced:10.0.0.3": true}
	if len(firing) != len(want) {
		t.Fatalf("firing %v", firing)
	}
	for k := range want {
		if !firing[k] {
			t.Errorf("missing %s in %v", k, firing)
		}
	}
}
