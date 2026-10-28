package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLooksLikeJWT(t *testing.T) {
	if !looksLikeJWT("eyJhbGciOiJIUzI1NiJ9.aaa.bbb") {
		t.Fatal("platform jwt")
	}
	if !looksLikeJWT("Bearer eyJhbGciOiJIUzI1NiJ9.aaa.bbb") {
		t.Fatal("bearer jwt")
	}
	if looksLikeJWT(`{"accessToken":"abc"}`) {
		t.Fatal("nacos json must not look like jwt")
	}
	if looksLikeJWT("a1b2c3d4-e5f6-7890-abcd-ef1234567890") {
		t.Fatal("uuid accessToken must not look like jwt")
	}
}

func TestPickAuthCredentialEmbedPrefersCookieEvenIfAuthIsJWT(t *testing.T) {
	// 与线上一致：Authorization 是 Nacos 签发的 eyJ JWT，Cookie 才是平台会话。
	platform := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.platform.sig"
	nacos := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJuYWNvcyIsImV4cCI6MTc5MzE4NjM5NX0.o2_H1TJ50zjwJtuoD5Tw0IPM7R3C1vrweflt_F4eBBU"
	got := pickAuthCredential("/api/v1/nacos-proxy/test/v3/console/server/state", "Bearer "+nacos, platform, "")
	if got != platform {
		t.Fatalf("embed proxy must use cookie, got %q", got)
	}
	got = pickAuthCredential("/api/v1/auth/me", "Bearer "+platform, "other", "")
	if got != platform {
		t.Fatalf("normal api prefer header jwt, got %q", got)
	}
	got = pickAuthCredential("/api/v1/nacosadmin", "Bearer "+`{"accessToken":"x"}`, platform, "")
	if got != platform {
		t.Fatalf("non-jwt header should fall back to cookie on normal path, got %q", got)
	}
}

func TestAuthNacosProxyAcceptsPlatformCookieWhenAuthIsNacosJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "unit-test-jwt-secret"
	platformJWT, err := SignToken(secret, "90396638-bf8b-4a2a-9599-8ddd9647221a", "admin", "admin", "tenant-default", "default", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// 真实抓包里的 Nacos accessToken（另一套密钥），外形也是 eyJ。
	nacosJWT := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJuYWNvcyIsImV4cCI6MTc5MzE4NjM5NX0.o2_H1TJ50zjwJtuoD5Tw0IPM7R3C1vrweflt_F4eBBU"

	r := gin.New()
	r.Use(Auth(&secret, nil, nil))
	r.GET("/api/v1/nacos-proxy/test/v3/console/server/state", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "user": c.GetString(CtxUsername)})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nacos-proxy/test/v3/console/server/state?username=nacos", nil)
	req.Header.Set("Authorization", "Bearer "+nacosJWT)
	req.Header.Set("accessToken", nacosJWT)
	req.AddCookie(&http.Cookie{Name: "token", Value: platformJWT})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["user"] != "admin" {
		t.Fatalf("want admin from platform cookie, got %#v", out)
	}

	// 对照组：仅 Nacos JWT、无 Cookie → 仍应 401
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/nacos-proxy/test/v3/console/server/state", nil)
	req2.Header.Set("Authorization", "Bearer "+nacosJWT)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("without cookie expect 401, got %d", w2.Code)
	}
}
