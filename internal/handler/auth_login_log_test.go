package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFirstForwardedIP(t *testing.T) {
	if got := firstForwardedIP("10.0.0.8, 192.168.1.1"); got != "10.0.0.8" {
		t.Fatalf("got %q", got)
	}
	if got := firstForwardedIP("127.0.0.1, 10.1.2.3"); got != "10.1.2.3" {
		t.Fatalf("skip loopback got %q", got)
	}
	if got := firstForwardedIP("127.0.0.1"); got != "" {
		t.Fatalf("only loopback got %q", got)
	}
	if got := firstForwardedIP(""); got != "" {
		t.Fatalf("empty got %q", got)
	}
}

func TestClientLoginIPSkipsLoopback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.RemoteAddr = "127.0.0.1:52344"
	c.Request = req

	got := clientLoginIP(c)
	if got == "" || isLoopbackIP(got) {
		t.Fatalf("expected LAN ip, got %q", got)
	}
}

func TestClientLoginIPPrefersForwarded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	req.Header.Set("X-Forwarded-For", "127.0.0.1, 10.20.30.40")
	req.RemoteAddr = "127.0.0.1:8800"
	c.Request = req
	if got := clientLoginIP(c); got != "10.20.30.40" {
		t.Fatalf("got %q", got)
	}
}

func TestStripPort(t *testing.T) {
	if got := stripPort("127.0.0.1:52344"); got != "127.0.0.1" {
		t.Fatalf("got %q", got)
	}
	if got := stripPort("10.1.2.3"); got != "10.1.2.3" {
		t.Fatalf("got %q", got)
	}
}
