package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type idleSrc struct{ mins int }

func (s idleSrc) LoginIdleTimeoutMin(def int) int {
	if s.mins < 0 {
		return def
	}
	return s.mins
}

func TestIdleSessionTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ClearIdle("u1")
	TouchIdle("u1")

	// 回拨 lastSeen 模拟超时
	idleMu.Lock()
	idleLastSeen["u1"] = time.Now().Add(-2 * time.Minute)
	idleMu.Unlock()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxUserID, "u1")
		c.Next()
	})
	r.Use(IdleSession(idleSrc{mins: 1}))
	r.GET("/x", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestIdleSessionDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ClearIdle("u2")
	idleMu.Lock()
	idleLastSeen["u2"] = time.Now().Add(-2 * time.Hour)
	idleMu.Unlock()

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(CtxUserID, "u2")
		c.Next()
	})
	r.Use(IdleSession(idleSrc{mins: 0}))
	r.GET("/x", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 when idle disabled, got %d", w.Code)
	}
}
