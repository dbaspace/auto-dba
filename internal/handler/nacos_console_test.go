package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestNacosTokenStorageShimIsolatesKey(t *testing.T) {
	if !strings.Contains(nacosTokenStorageShim, "nacos_embed_token") {
		t.Fatal("shim must redirect to nacos_embed_token")
	}
	if !strings.Contains(nacosTokenStorageShim, "auto-dba-nacos-token-shim") {
		t.Fatal("shim needs stable id for idempotent inject")
	}
	// 平台 JWT 键仍叫 token，shim 只在 localStorage 上重定向该键。
	if !strings.Contains(nacosTokenStorageShim, "k === 'token'") && !strings.Contains(nacosTokenStorageShim, `k === "token"`) {
		t.Fatal("shim must intercept token key")
	}
}

func TestRewriteNacosBodyInjectsTokenShim(t *testing.T) {
	raw := []byte(`<!DOCTYPE html><html><head><title>x</title></head><body>hi</body></html>`)
	out, did := rewriteNacosBody(raw, "text/html", "", "/api/v1/nacos-proxy/test")
	if !did {
		t.Fatal("expected rewrite")
	}
	s := string(out)
	if !strings.Contains(s, "auto-dba-nacos-token-shim") {
		t.Fatal("missing token shim")
	}
	if !strings.Contains(s, "auto-dba-nacos-embed") {
		t.Fatal("missing embed chrome")
	}
	if strings.Contains(s, "frame-ancestors") || strings.Contains(strings.ToLower(s), "content-security-policy") {
		t.Fatal("CSP frame-ancestors must not be injected via meta; use response header")
	}
	if strings.Contains(nacosEmbedChrome, "Content-Security-Policy") || strings.Contains(nacosEmbedChrome, "frame-ancestors") {
		t.Fatal("nacosEmbedChrome must not contain CSP meta")
	}
	// 二次改写幂等
	out2, did2 := rewriteNacosBody(out, "text/html", "", "/api/v1/nacos-proxy/test")
	if did2 && strings.Count(string(out2), "auto-dba-nacos-token-shim") != 1 {
		t.Fatalf("shim should inject once, count=%d", strings.Count(string(out2), "auto-dba-nacos-token-shim"))
	}
}

func TestSetNacosEmbedFrameAncestorsHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	setNacosEmbedFrameAncestors(c)
	if got := w.Header().Get("Content-Security-Policy"); got != nacosFrameAncestorsCSP {
		t.Fatalf("CSP header=%q want %q", got, nacosFrameAncestorsCSP)
	}
}

func TestNacosSplitConsoleURL(t *testing.T) {
	origin, path, err := nacosSplitConsoleURL("http://10.100.113.151:8080/next")
	if err != nil {
		t.Fatal(err)
	}
	if origin != "http://10.100.113.151:8080" || path != "/next" {
		t.Fatalf("got origin=%q path=%q", origin, path)
	}
	origin, path, err = nacosSplitConsoleURL("http://10.100.113.151:8080/next/")
	if err != nil {
		t.Fatal(err)
	}
	if origin != "http://10.100.113.151:8080" || path != "/next" {
		t.Fatalf("trailing slash: origin=%q path=%q", origin, path)
	}
	origin, path, err = nacosSplitConsoleURL("http://10.100.113.151:8080")
	if err != nil {
		t.Fatal(err)
	}
	if origin != "http://10.100.113.151:8080" || path != "/next" {
		t.Fatalf("default next: origin=%q path=%q", origin, path)
	}
	if _, _, err := nacosSplitConsoleURL("not-a-url"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNacosProxyEnvPrefix(t *testing.T) {
	if got := nacosProxyEnvPrefix("test"); got != "/api/v1/nacos-proxy/test" {
		t.Fatalf("got %q", got)
	}
}

func TestShouldRewriteNacosBodyStaticNotRewritten(t *testing.T) {
	if shouldRewriteNacosBody("application/javascript", "/next/js/vendor-monaco.js") {
		t.Fatal("js must not rewrite")
	}
	if shouldRewriteNacosBody("text/css", "/next/css/main.css") {
		t.Fatal("css must not rewrite")
	}
	if !shouldRewriteNacosBody("text/html", "/next/") {
		t.Fatal("html must rewrite")
	}
	if !shouldRewriteNacosBody("application/json", "/v3/auth/user") {
		t.Fatal("json must rewrite for origin replace")
	}
}

func TestResolveNacosLocalStatic(t *testing.T) {
	root := t.TempDir()
	jsDir := filepath.Join(root, "next", "js")
	if err := os.MkdirAll(jsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(jsDir, "main.js")
	if err := os.WriteFile(target, []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, ok := resolveNacosLocalStatic(root, "/next/js/main.js")
	if !ok {
		t.Fatal("expected hit")
	}
	if abs != target {
		// macOS may differ by symlink; compare basenames via Rel
		if filepath.Base(abs) != "main.js" {
			t.Fatalf("abs=%q", abs)
		}
	}
	if _, ok := resolveNacosLocalStatic(root, "/next/js/missing.js"); ok {
		t.Fatal("missing should miss")
	}
	if _, ok := resolveNacosLocalStatic(root, "/next/js/../../etc/passwd"); ok {
		t.Fatal("traversal must fail")
	}
	if _, ok := resolveNacosLocalStatic(root, "/next/"); ok {
		t.Fatal("html path must not be treated as static file")
	}
	if _, ok := resolveNacosLocalStatic("", "/next/js/main.js"); ok {
		t.Fatal("empty static dir disables local")
	}
}

func TestTryServeNacosLocalStatic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	jsDir := filepath.Join(root, "next", "js")
	if err := os.MkdirAll(jsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("/* monaco */")
	if err := os.WriteFile(filepath.Join(jsDir, "vendor-monaco.js"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	h := &NacosAdminHandler{StaticDir: root}
	r := gin.New()
	r.GET("/proxy/*filepath", func(c *gin.Context) {
		rest := c.Param("filepath")
		if !h.tryServeNacosLocalStatic(c, "test", rest, time.Now()) {
			c.Status(http.StatusNotFound)
		}
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/next/js/vendor-monaco.js", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Header().Get("X-Nacos-Static") != "local" {
		t.Fatal("missing local header")
	}
	if got := w.Body.String(); got != string(body) {
		t.Fatalf("body=%q", got)
	}
}

func TestResolveNacosLocalStaticRealAssets(t *testing.T) {
	root := filepath.Join("..", "..", "assets", "nacos-console")
	if _, err := os.Stat(filepath.Join(root, "next", "js", "vendor-monaco.js")); err != nil {
		t.Skip("assets not synced:", err)
	}
	abs, ok := resolveNacosLocalStatic(root, "/next/js/vendor-monaco.js")
	if !ok {
		t.Fatal("expected local monaco hit")
	}
	st, err := os.Stat(abs)
	if err != nil || st.Size() < 1_000_000 {
		t.Fatalf("monaco unexpected: err=%v size=%d", err, st.Size())
	}
	css, ok := resolveNacosLocalStatic(root, "/next/css/main.css")
	if !ok {
		t.Fatal("expected local css hit")
	}
	t.Logf("monaco=%s css=%s", abs, css)

	gin.SetMode(gin.TestMode)
	h := &NacosAdminHandler{StaticDir: root}
	r := gin.New()
	r.GET("/*filepath", func(c *gin.Context) {
		if !h.tryServeNacosLocalStatic(c, "test", c.Param("filepath"), time.Now()) {
			c.Status(http.StatusNotFound)
		}
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/next/js/vendor-monaco.js", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("X-Nacos-Static") != "local" {
		t.Fatalf("serve local failed status=%d hdr=%q", w.Code, w.Header().Get("X-Nacos-Static"))
	}
	if w.Body.Len() != int(st.Size()) {
		t.Fatalf("body=%d want=%d", w.Body.Len(), st.Size())
	}
}
