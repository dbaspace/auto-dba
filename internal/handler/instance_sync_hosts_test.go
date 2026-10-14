package handler

import "testing"

func TestHostFromMonitorEndpoint(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"10.0.0.1:9100", "10.0.0.1"},
		{"http://10.0.0.2:8030/metrics", "10.0.0.2"},
		{"fe.example.com:8030", "fe.example.com"},
		{"192.168.1.10", "192.168.1.10"},
		{"[2001:db8::1]:9100", "2001:db8::1"},
		{"", ""},
	}
	for _, c := range cases {
		if got := hostFromMonitorEndpoint(c.in); got != c.want {
			t.Fatalf("hostFromMonitorEndpoint(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestSyncNormHost(t *testing.T) {
	if got := syncNormHost("10.0.0.1"); got != "10.0.0.1" {
		t.Fatalf("got %q", got)
	}
	if got := syncNormHost("Fe.Example.COM."); got != "fe.example.com" {
		t.Fatalf("got %q", got)
	}
}
