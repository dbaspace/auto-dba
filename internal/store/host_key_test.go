package store

import "testing"

func TestHostAddrKey(t *testing.T) {
	cases := map[string]string{
		"10.0.0.1":              "10.0.0.1",
		" 10.0.0.1:22 ":         "10.0.0.1",
		"10.0.0.1,10.0.0.2":     "10.0.0.1",
		"10.0.0.1，10.0.0.2":     "10.0.0.1",
		"Host-A.Example.com":    "host-a.example.com",
		"host-a.example.com:22": "host-a.example.com",
		"[::1]:22":              "::1",
		"http://10.0.0.3:9100/": "10.0.0.3",
		"":                      "",
	}
	for in, want := range cases {
		if got := HostAddrKey(in); got != want {
			t.Errorf("HostAddrKey(%q)=%q want %q", in, got, want)
		}
	}
}
