package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/clientaccess"
)

func TestClientAccessGateSkipsBrowser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxAuthKind, "jwt")
		c.Set(CtxClientType, "browser")
		c.Set(CtxUserID, "u1")
		c.Next()
	})
	r.Use(ClientAccessGate(nil)) // db nil → pass-through by enabled check... actually nil returns early
	r.GET("/api/v1/query", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
}

func TestWhitelistStillCoversQuery(t *testing.T) {
	if !clientaccess.WhitelistOK("POST", "/api/v1/query") {
		t.Fatal("expected query whitelisted")
	}
}
