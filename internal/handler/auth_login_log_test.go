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

func TestLoginClientAddrsSplitsPublicAndLAN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func(xff, realIP string) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		if realIP != "" {
			req.Header.Set("X-Real-IP", realIP)
		}
		req.RemoteAddr = "127.0.0.1:8800"
		c.Request = req
		return c
	}

	ip, lan := loginClientAddrs(newCtx("10.1.2.3, 61.49.49.50", ""))
	if ip != "61.49.49.50" || lan != "10.1.2.3" {
		t.Fatalf("chain got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginClientAddrs(newCtx("61.49.49.50", ""))
	if ip != "61.49.49.50" || lan != "" {
		t.Fatalf("public only got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginClientAddrs(newCtx("61.49.49.50", "10.8.8.8"))
	if ip != "61.49.49.50" || lan != "10.8.8.8" {
		t.Fatalf("real-ip got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginClientAddrs(newCtx("10.10.102.163", ""))
	if ip != "" || lan != "10.10.102.163" {
		t.Fatalf("lan only got ip=%q lan=%q", ip, lan)
	}
}

func TestReportedPublicIPOverridesLAN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func(xff, reported string) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		req.RemoteAddr = "127.0.0.1:8800"
		c.Request = req
		setReportedPublicIP(c, reported)
		return c
	}

	ip, lan := loginLogAddrs(newCtx("10.10.102.163", "1.2.3.4"))
	if ip != "1.2.3.4" || lan != "10.10.102.163" {
		t.Fatalf("reported public keeps request lan got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginLogAddrs(newCtx("10.10.102.163", "10.1.1.1"))
	if ip != "" || lan != "10.10.102.163" {
		t.Fatalf("private public report ignored got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginLogAddrs(newCtx("61.49.49.50", "203.0.113.8"))
	if ip != "203.0.113.8" || lan != "" {
		t.Fatalf("reported overrides gateway got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginLogAddrs(newCtx("10.110.16.33, 61.49.49.50", ""))
	if ip != "61.49.49.50" || lan != "10.110.16.33" {
		t.Fatalf("no report keeps request lan got ip=%q lan=%q", ip, lan)
	}
}

func TestReportedLanIPOverridesProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func(xff, reportedLan string) *gin.Context {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		req.RemoteAddr = "10.110.16.33:8800"
		c.Request = req
		setReportedLanIP(c, reportedLan)
		return c
	}

	ip, lan := loginLogAddrs(newCtx("10.110.16.33, 61.49.49.50", "10.10.102.163"))
	if ip != "61.49.49.50" || lan != "10.10.102.163" {
		t.Fatalf("reported lan overrides ingress got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginLogAddrs(newCtx("10.110.16.33, 61.49.49.50", "61.49.49.50"))
	if ip != "61.49.49.50" || lan != "10.110.16.33" {
		t.Fatalf("public lan dropped got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginLogAddrs(newCtx("10.110.16.33, 61.49.49.50", "127.0.0.1"))
	if ip != "61.49.49.50" || lan != "10.110.16.33" {
		t.Fatalf("loopback lan dropped got ip=%q lan=%q", ip, lan)
	}
	ip, lan = loginLogAddrs(newCtx("10.110.16.33, 61.49.49.50", "169.254.1.1"))
	if ip != "61.49.49.50" || lan != "10.110.16.33" {
		t.Fatalf("link-local lan dropped got ip=%q lan=%q", ip, lan)
	}
	if got := sanitizeLanIP("10.10.102.163:1234"); got != "10.10.102.163" {
		t.Fatalf("strip port got %q", got)
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
