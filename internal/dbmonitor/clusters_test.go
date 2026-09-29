package dbmonitor

import "testing"

func TestClassifyStarRocksEndpoint(t *testing.T) {
	cases := []struct {
		ep, app, want string
	}{
		{"10.128.20.117:8030", "fe-StarRocks", "fe"},
		{"10.128.16.6:8040", "fe-StarRocks", "be"}, // 误标成 fe，端口判 BE
		{"10.0.0.1:8040", "be-StarRocks", "be"},
		{"10.0.0.2:9010", "", "fe"},
		{"10.0.0.3:9060", "", "be"},
		{"10.0.0.4:9100", "fe-StarRocks", "fe"},
		{"10.0.0.5:9100", "be-StarRocks", "be"},
	}
	for _, c := range cases {
		got := classifyStarRocksEndpoint(c.ep, c.app, map[string]string{"app": c.app})
		if got != c.want {
			t.Fatalf("ep=%s app=%s got=%s want=%s", c.ep, c.app, got, c.want)
		}
	}
}
