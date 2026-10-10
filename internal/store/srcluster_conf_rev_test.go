package store

import (
	"testing"
	"time"
)

func TestSrConfRevNowFixedWidth(t *testing.T) {
	a := srConfRevNow()
	time.Sleep(2 * time.Microsecond)
	b := srConfRevNow()
	if len(a) != len(b) || len(a) != len("2006-01-02T15:04:05.000000Z") {
		t.Fatalf("时间戳须定宽: %q %q", a, b)
	}
	if !(a < b) {
		t.Fatalf("字典序须与时间序一致: %q !< %q", a, b)
	}
}

func TestSrConfContentHash(t *testing.T) {
	if SrConfContentHash("a") == SrConfContentHash("b") || len(SrConfContentHash("a")) != 16 {
		t.Fatal("hash 异常")
	}
	if !validSrConfRevKind(SrConfRevApplied) || validSrConfRevKind("x") {
		t.Fatal("kind 校验异常")
	}
}
