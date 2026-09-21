package redisquery

import "testing"

func TestParseDBIndex(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"", 0, false},
		{"db0", 0, false},
		{"DB3", 3, false},
		{"15", 15, false},
		{"db16", 0, true},
		{"foo", 0, true},
	}
	for _, c := range cases {
		got, err := ParseDBIndex(c.in)
		if c.wantErr {
			if err == nil {
				t.Fatalf("%q should fail", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("%q got %d err=%v want %d", c.in, got, err, c.want)
		}
	}
}

func TestAddr(t *testing.T) {
	if got := Addr(nil); got != "localhost:6379" {
		t.Fatalf("nil addr %s", got)
	}
}
