package server

import (
	"strings"
	"testing"
	"time"
)

func TestRedactRequestPath(t *testing.T) {
	in := "/api/v1/terminal/ws?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.abc&x=1"
	out := redactRequestPath(in)
	if strings.Contains(out, "eyJ") {
		t.Fatalf("token still present: %s", out)
	}
	if !strings.Contains(out, "token=%5Bredacted%5D") && !strings.Contains(out, "token=[redacted]") {
		t.Fatalf("expected redacted token, got %s", out)
	}
	if !strings.Contains(out, "x=1") {
		t.Fatalf("other query lost: %s", out)
	}
}

func TestIsTerminalWSPath(t *testing.T) {
	if !isTerminalWSPath("/api/v1/terminal/ws") {
		t.Fatal("expected true")
	}
	if !isTerminalWSPath("/api/v1/terminal/ws?token=x") {
		t.Fatal("expected true with query")
	}
	if isTerminalWSPath("/api/v1/instances") {
		t.Fatal("expected false")
	}
}

func TestAccessLogThrottle(t *testing.T) {
	th := newAccessLogThrottle()
	ok, _ := th.allow("ip|/api/v1/terminal/ws", time.Minute)
	if !ok {
		t.Fatal("first should allow")
	}
	ok, _ = th.allow("ip|/api/v1/terminal/ws", time.Minute)
	if ok {
		t.Fatal("second within window should block")
	}
	ok, suppressed := th.allow("ip|/api/v1/terminal/ws", time.Minute)
	if ok {
		t.Fatal("third should still block")
	}
	_ = suppressed
	// 模拟窗口过期
	th.mu.Lock()
	e := th.last["ip|/api/v1/terminal/ws"]
	e.at = time.Now().Add(-2 * time.Minute)
	e.extra = 5
	th.last["ip|/api/v1/terminal/ws"] = e
	th.mu.Unlock()
	ok, suppressed = th.allow("ip|/api/v1/terminal/ws", time.Minute)
	if !ok {
		t.Fatal("after window should allow")
	}
	if suppressed != 5 {
		t.Fatalf("suppressed want 5 got %d", suppressed)
	}
}
