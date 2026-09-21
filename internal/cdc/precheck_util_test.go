package cdc

import (
	"strings"
	"testing"
)

func TestJoinSample(t *testing.T) {
	if got := JoinSample(nil, 20); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := JoinSample([]string{"a", "b", "c"}, 20); got != "a,b,c" {
		t.Fatalf("short: %q", got)
	}
	many := make([]string, 25)
	for i := range many {
		many[i] = string(rune('a'+i%26)) + string(rune('0'+i/26))
	}
	got := JoinSample(many, 5)
	if !strings.Contains(got, "等共 25 项") {
		t.Fatalf("got %q", got)
	}
	parts := strings.Split(got, " 等共")
	if len(strings.Split(parts[0], ",")) != 5 {
		t.Fatalf("expected 5 samples: %q", got)
	}
}
