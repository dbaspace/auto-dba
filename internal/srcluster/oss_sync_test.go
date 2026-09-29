package srcluster

import "testing"

func TestFlatNameMatchesVersionRole(t *testing.T) {
	cases := []struct {
		name, ver, role string
		want            bool
	}{
		{"StarRocks-3.2.10.fe.tar.gz", "3.2.10", "fe", true},
		{"StarRocks-3.2.10.be.tar.gz", "3.2.10", "be", true},
		{"StarRocks-3.2.10.be.tar.gz", "3.2.10", "fe", false},
		{"StarRocks-3.2.10.fe.tar.gz", "3.2.1", "fe", false},
		{"StarRocks-3.2.10-fe.tar.gz", "3.2.10", "fe", true},
		{"other-3.2.10.fe.tgz", "3.2.10", "fe", true},
		{"readme.txt", "3.2.10", "fe", false},
	}
	for _, c := range cases {
		got := flatNameMatchesVersionRole(c.name, c.ver, c.role)
		if got != c.want {
			t.Fatalf("%s ver=%s role=%s: got %v want %v", c.name, c.ver, c.role, got, c.want)
		}
	}
}
