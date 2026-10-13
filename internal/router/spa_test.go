package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServeSPA_MissingAssetNoFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>index</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app-abc.js"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := gin.New()
	serveSPA(e, root)

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/old-hash.js", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing .js: status=%d body=%q", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "index") {
		t.Fatalf("missing asset must not fall back to index.html: %q", w.Body.String())
	}
}

func TestServeSPA_RouteFallsBackToIndexNoCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>index</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := gin.New()
	serveSPA(e, root)

	for _, path := range []string{"/", "/alerts/active", "/aiops/incidents"} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status=%d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "index") {
			t.Fatalf("%s: body=%q", path, w.Body.String())
		}
		cc := w.Header().Get("Cache-Control")
		if !strings.Contains(cc, "no-cache") {
			t.Fatalf("%s: Cache-Control=%q want no-cache", path, cc)
		}
	}
}

func TestServeSPA_ExistingAssetLongCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>index</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app-abc.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := gin.New()
	serveSPA(e, root)

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if w.Body.String() != "console.log(1)" {
		t.Fatalf("body=%q", w.Body.String())
	}
	cc := w.Header().Get("Cache-Control")
	if !strings.Contains(cc, "max-age=31536000") {
		t.Fatalf("Cache-Control=%q", cc)
	}
}

func TestSafeSPAJoin(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	full, ok := safeSPAJoin(root, "/assets/x.js")
	if !ok || !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		t.Fatalf("join: full=%q ok=%v", full, ok)
	}
	full, ok = safeSPAJoin(root, "/../../etc/passwd")
	if !ok {
		t.Fatal("cleaned traversal should still resolve under root")
	}
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		t.Fatalf("escaped root: %q", full)
	}
	full, ok = safeSPAJoin(root, "/")
	if !ok || full != root {
		t.Fatalf("root path: full=%q ok=%v", full, ok)
	}
}
