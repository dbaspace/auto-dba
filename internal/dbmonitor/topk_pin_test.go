package dbmonitor

import (
	"strings"
	"testing"
)

func TestSplitOuterTopK(t *testing.T) {
	k, inner, ok := splitOuterTopK(`topk(12, sum by (datname) (rate(x{a="b"}[5m])))`)
	if !ok || k != 12 {
		t.Fatalf("got ok=%v k=%d", ok, k)
	}
	if !strings.Contains(inner, `sum by (datname)`) || !strings.Contains(inner, `rate(x{a="b"}[5m])`) {
		t.Fatalf("inner=%q", inner)
	}
	if _, _, ok := splitOuterTopK(`sum by (datname) (x)`); ok {
		t.Fatal("expected not topk")
	}
}

func TestInjectLabelMatcher(t *testing.T) {
	got := injectLabelMatcher(`rate(x{idc="t"}[5m]) + rate(y{idc="t"}[5m])`, `datname=~"a|b"`)
	if strings.Count(got, `datname=~"a|b"`) != 2 {
		t.Fatalf("got=%q", got)
	}
}
