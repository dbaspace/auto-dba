package ticketexec

import "testing"

func TestParseStarRocksUserIdentity(t *testing.T) {
	cases := map[string][2]string{
		"'root'@'%'":          {"root", "%"},
		"'report'@'10.0.0.1'": {"report", "10.0.0.1"},
		"'o''k'@'%'":          {"o'k", "%"},
		"plain":               {"plain", "%"},
	}
	for in, want := range cases {
		u, h := parseStarRocksUserIdentity(in)
		if u != want[0] || h != want[1] {
			t.Fatalf("%s => %s@%s", in, u, h)
		}
	}
}
