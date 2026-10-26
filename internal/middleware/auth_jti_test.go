package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"auto-dba/internal/sessionstore"
)

func TestSignSessionTokenHasJTI(t *testing.T) {
	tok, jti, err := SignSessionToken("sec", "u1", "admin", "admin", "t1", "c1", false, time.Hour)
	if err != nil || jti == "" || tok == "" {
		t.Fatalf("tok=%v jti=%v err=%v", tok, jti, err)
	}
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) {
		return []byte("sec"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatal(err)
	}
	if claims.ID != jti {
		t.Fatalf("claims.ID=%q jti=%q", claims.ID, jti)
	}
	// 兼容旧 SignToken 签名
	tok2, err := SignToken("sec", "u2", "u", "admin", "", "", time.Hour)
	if err != nil || tok2 == "" {
		t.Fatal(err)
	}
}

func TestTokenIssuedBeforePasswordChange(t *testing.T) {
	issued := jwt.NewNumericDate(time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC))
	if tokenIssuedBeforePasswordChange(issued, "") {
		t.Fatal("empty changed-at should keep the session")
	}
	if tokenIssuedBeforePasswordChange(issued, "not-a-time") {
		t.Fatal("bad timestamp should not kick")
	}
	if tokenIssuedBeforePasswordChange(issued, "2026-10-22T00:00:00Z") {
		t.Fatal("change before issue should keep the session")
	}
	if !tokenIssuedBeforePasswordChange(issued, "2026-10-22T02:00:00Z") {
		t.Fatal("change after issue should revoke")
	}
	if !tokenIssuedBeforePasswordChange(nil, "2026-10-22T02:00:00Z") {
		t.Fatal("missing issued-at should revoke after a password change")
	}
}

func TestResignSessionTokenKeepsJTI(t *testing.T) {
	tok, jti, err := SignSessionToken("sec", "u1", "admin", "admin", "t1", "c1", false, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims := &Claims{}
	if _, err := jwt.ParseWithClaims(tok, claims, func(t *jwt.Token) (any, error) {
		return []byte("sec"), nil
	}); err != nil && claims.ID == "" {
		t.Fatal(err)
	}
	renewed, err := ResignSessionToken("sec", claims, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	next := &Claims{}
	parsed, err := jwt.ParseWithClaims(renewed, next, func(t *jwt.Token) (any, error) {
		return []byte("sec"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatal(err)
	}
	if next.ID != jti {
		t.Fatalf("jti=%q want %q", next.ID, jti)
	}
	if next.ExpiresAt == nil || next.ExpiresAt.Time.Before(time.Now().Add(23*time.Hour)) {
		t.Fatalf("exp=%v", next.ExpiresAt)
	}
}

type ttlGate struct{ ttl time.Duration }

func (g ttlGate) Enabled() bool  { return false }
func (g ttlGate) FailOpen() bool { return false }
func (g ttlGate) Valid(context.Context, string, string) (bool, error) {
	return true, nil
}
func (g ttlGate) SessionTTL() time.Duration { return g.ttl }

type expiredGate struct{}

func (expiredGate) Enabled() bool  { return true }
func (expiredGate) FailOpen() bool { return false }
func (expiredGate) Valid(context.Context, string, string) (bool, error) {
	return false, sessionstore.ErrSessionExpired
}

func TestAuthRenewsTokenWhenRemainingUnder12h(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "sec"
	tok, jti, err := SignSessionToken(secret, "u1", "admin", "admin", "", "", false, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(Auth(&secret, nil, ttlGate{ttl: 24 * time.Hour}))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	got := w.Header().Get("X-Access-Token")
	if got == "" {
		t.Fatal("missing renewed token")
	}
	claims := &Claims{}
	if _, err := jwt.ParseWithClaims(got, claims, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	}); err != nil {
		t.Fatal(err)
	}
	if claims.ID != jti {
		t.Fatalf("jti=%q want %q", claims.ID, jti)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(time.Now().Add(23*time.Hour)) {
		t.Fatalf("exp=%v", claims.ExpiresAt)
	}
}

func TestAuthKeepsTokenWhenRemainingOver12h(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "sec"
	tok, _, err := SignSessionToken(secret, "u1", "admin", "admin", "", "", false, 20*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(Auth(&secret, nil, nil))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Access-Token"); got != "" {
		t.Fatal("should not renew when remaining is over 12h")
	}
}

func TestAuthExpiredTokenPromptsRelogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "sec"
	tok, _, err := SignSessionToken(secret, "u1", "admin", "admin", "", "", false, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(Auth(&secret, nil, nil))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "session_expired" || body["message"] != "登录已过期，请重新登录" {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestAuthRedisExpiredPromptsRelogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "sec"
	tok, _, err := SignSessionToken(secret, "u1", "admin", "admin", "", "", false, 20*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(Auth(&secret, nil, expiredGate{}))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "session_expired" || body["message"] != "登录已过期，请重新登录" {
		t.Fatalf("body=%s", w.Body.String())
	}
	if got := w.Header().Get("X-Access-Token"); got != "" {
		t.Fatal("expired session must not renew")
	}
}
