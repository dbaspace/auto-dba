package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
)

func reinitTestEngine(role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxRole, role)
		c.Set(middleware.CtxUsername, "alice")
		c.Next()
	})
	return r
}

func TestReinitForbiddenForNonAdminOutsideWhitelist(t *testing.T) {
	h := &CDCHandler{}
	r := reinitTestEngine("developer")
	r.POST("/tasks/:id/reinit/preview", h.ReinitPreview)
	r.POST("/tasks/:id/reinit", h.Reinit)
	r.POST("/tasks/reinit/batch", h.ReinitBatch)
	for _, path := range []string{"/tasks/x/reinit/preview", "/tasks/x/reinit", "/tasks/reinit/batch"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d", path, w.Code)
		}
		var body map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["message"] != cdcReinitForbidden {
			t.Fatalf("%s: message=%q", path, body["message"])
		}
	}
}

func TestReinitAdminAllowed(t *testing.T) {
	h := &CDCHandler{}
	for _, role := range []string{"admin", "super_admin", "platform_admin"} {
		r := reinitTestEngine(role)
		var ok bool
		r.GET("/x", func(c *gin.Context) { ok = h.canReinitUser(c) })
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
		if !ok {
			t.Fatalf("role %s should be allowed", role)
		}
	}
}

func TestReinitRoutesDoNotConflictWithTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	mark := func(name string) gin.HandlerFunc { return func(c *gin.Context) { c.String(200, name) } }
	r.GET("/tasks/init-list", mark("init-list"))
	r.GET("/tasks/reinit/candidates", mark("candidates"))
	r.POST("/tasks/reinit/batch", mark("batch"))
	r.GET("/tasks/:id", mark("get"))
	r.POST("/tasks/:id/retry", mark("retry"))
	r.POST("/tasks/:id/reinit/preview", mark("preview"))
	r.POST("/tasks/:id/reinit", mark("reinit"))
	cases := map[string]string{
		"GET /tasks/reinit/candidates":   "candidates",
		"POST /tasks/reinit/batch":       "batch",
		"GET /tasks/abc":                 "get",
		"POST /tasks/abc/reinit":         "reinit",
		"POST /tasks/abc/reinit/preview": "preview",
	}
	for req, want := range cases {
		parts := strings.SplitN(req, " ", 2)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(parts[0], parts[1], nil))
		if w.Body.String() != want {
			t.Fatalf("%s -> %q want %q", req, w.Body.String(), want)
		}
	}
}
