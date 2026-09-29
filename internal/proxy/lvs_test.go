package proxy

import (
	"strings"
	"testing"
)

func TestRenderKeepalivedConf_DR(t *testing.T) {
	opt := DefaultLVSOptions("mysql-rw")
	opt.State = "MASTER"
	opt.Priority = 150
	opt.LbKind = "DR"
	opt.LbAlgo = "wrr"
	opt.Interface = "ens192"
	content, err := RenderKeepalivedConf("mysql-rw", "10.0.0.100", 3306, []Backend{
		{Host: "10.0.0.11", Port: 3306, Weight: 100},
		{Host: "10.0.0.12", Port: 3306, Weight: 50},
	}, opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"global_defs",
		"vrrp_instance VI_mysql_rw",
		"state MASTER",
		"interface ens192",
		"virtual_router_id 51",
		"priority 150",
		"10.0.0.100/32 dev ens192",
		"virtual_server 10.0.0.100 3306",
		"lb_algo wrr",
		"lb_kind DR",
		"real_server 10.0.0.11 3306",
		"inhibit_on_failure",
		"TCP_CHECK",
		"arp_ignore",
	} {
		if !contains(content, want) {
			t.Fatalf("missing %q in:\n%s", want, content)
		}
	}
}

func TestRenderKeepalivedConf_NAT(t *testing.T) {
	opt := DefaultLVSOptions("app")
	opt.LbKind = "NAT"
	opt.PersistenceTimeout = 50
	opt.SorryServer = "10.0.0.99:3306"
	content, err := RenderKeepalivedConf("app", "192.168.1.10", 3306, []Backend{
		{Host: "192.168.1.2", Port: 3306, Weight: 2},
	}, opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lb_kind NAT", "persistence_timeout 50", "sorry_server 10.0.0.99 3306"} {
		if !contains(content, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestNormalizeLVSOptions_AuthPassTooLong(t *testing.T) {
	opt := DefaultLVSOptions("x")
	opt.AuthPass = "toolongpass"
	_, err := NormalizeLVSOptions(opt, "x")
	if err == nil {
		t.Fatal("expected auth_pass length error")
	}
}

func TestValidateVIP(t *testing.T) {
	if ValidateVIP("10.0.0.1") != nil {
		t.Fatal("valid vip")
	}
	if ValidateVIP("10.0.0.1/32") != nil {
		t.Fatal("valid vip/cidr")
	}
	if ValidateVIP("bad") == nil {
		t.Fatal("expected error")
	}
	if ValidateVIP("") == nil {
		t.Fatal("expected empty error")
	}
}

func TestBuildKeepalivedConf_VRRPOnly(t *testing.T) {
	s, err := BuildKeepalivedConf("haproxy", "rw", "10.2.2.10", 3306, nil, &LVSOptions{
		Interface: "eth0", State: "BACKUP", VirtualRouterID: 77, Priority: 120,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(s, "vrrp_instance") || !contains(s, "10.2.2.10") || !contains(s, "virtual_router_id 77") {
		t.Fatalf("unexpected vrrp conf:\n%s", s)
	}
	if contains(s, "virtual_server") {
		t.Fatal("haproxy keepalived should be VRRP-only")
	}
	s2, err := BuildKeepalivedConf("nginx", "ngx", "10.2.2.11/32", 3306, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(s2, "10.2.2.11") || contains(s2, "virtual_server") {
		t.Fatalf("unexpected nginx ka:\n%s", s2)
	}
}

func TestAttachKeepalivedToOptions(t *testing.T) {
	raw := AttachKeepalivedToOptions("", DefaultKeepalivedPath, "ka-content", &LVSOptions{Interface: "ens3"})
	o := ParseProxyOptions(raw)
	if o.KeepalivedPath != DefaultKeepalivedPath || o.KeepalivedContent != "ka-content" {
		t.Fatalf("%+v", o)
	}
	if o.LVS == nil || o.LVS.Interface != "ens3" {
		t.Fatalf("lvs options: %+v", o.LVS)
	}
}

func TestRenderDefaultConf_LVSUsesKeepalived(t *testing.T) {
	s := RenderDefaultConf("lvs", RenderOptions{
		Name: "demo", VIP: "10.1.1.1", ListenPort: 3306,
		Backends: []Backend{{Host: "10.1.1.2", Port: 3306}},
		LVS:      &LVSOptions{LbKind: "TUN", Interface: "eth1", VirtualRouterID: 60},
	})
	if !contains(s, "lb_kind TUN") || !contains(s, "interface eth1") || !contains(s, "virtual_router_id 60") {
		t.Fatalf("unexpected conf:\n%s", s)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
