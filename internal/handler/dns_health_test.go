package handler

import (
	"testing"

	"auto-dba/internal/dns"
	"auto-dba/internal/promclient"
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

func TestRatioChecks(t *testing.T) {
	samples := []promclient.Sample{
		{Metric: map[string]string{"instance": "10.0.0.8:9153"}, Value: []any{1.0, "0.2"}},
		{Metric: map[string]string{"instance": "10.0.0.9:9153"}, Value: []any{1.0, "0.01"}},
		{Metric: map[string]string{"instance": ""}, Value: []any{1.0, "0.9"}},
	}
	got := ratioChecks(samples, dnsAlertKindFwd, 0.05, "DNS 上游转发失败偏高", "上游转发失败率")
	firing := map[string]string{}
	for _, ck := range got {
		if ck.Firing {
			firing[ck.IP] = ck.Kind
		}
	}
	if len(got) != 2 || len(firing) != 1 || firing["10.0.0.8"] != dnsAlertKindFwd {
		t.Fatalf("checks %+v firing %v", got, firing)
	}
}
