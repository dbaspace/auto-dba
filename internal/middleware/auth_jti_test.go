package middleware

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
