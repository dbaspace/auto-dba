package ssoauth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"auto-dba/internal/store"
)

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksCacheEntry struct {
	fetchedAt time.Time
	keys      map[string]*rsa.PublicKey
	anyKey    *rsa.PublicKey
}

var (
	jwksCacheMu sync.Mutex
	jwksCache   = map[string]jwksCacheEntry{}
)

func b64URLDecode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty")
	}
	if m := len(s) % 4; m != 0 {
		s += strings.Repeat("=", 4-m)
	}
	return base64.URLEncoding.DecodeString(s)
}

func rsaPublicFromJWK(k jwkKey) (*rsa.PublicKey, error) {
	if !strings.EqualFold(k.Kty, "RSA") {
		return nil, fmt.Errorf("unsupported kty %s", k.Kty)
	}
	nb, err := b64URLDecode(k.N)
	if err != nil {
		return nil, fmt.Errorf("jwk n: %w", err)
	}
	eb, err := b64URLDecode(k.E)
	if err != nil {
		return nil, fmt.Errorf("jwk e: %w", err)
	}
	var eInt int
	for _, b := range eb {
		eInt = eInt<<8 + int(b)
	}
	if eInt == 0 {
		eInt = 65537
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: eInt}, nil
}

func loadJWKS(jwksURL string) (map[string]*rsa.PublicKey, *rsa.PublicKey, error) {
	jwksURL = strings.TrimSpace(jwksURL)
	if jwksURL == "" {
		return nil, nil, errors.New("jwks_uri empty")
	}
	jwksCacheMu.Lock()
	if ent, ok := jwksCache[jwksURL]; ok && time.Since(ent.fetchedAt) < 10*time.Minute {
		jwksCacheMu.Unlock()
		return ent.keys, ent.anyKey, nil
	}
	jwksCacheMu.Unlock()

	req, err := http.NewRequest(http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("jwks http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var doc jwksDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, nil, fmt.Errorf("jwks json: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	var anyKey *rsa.PublicKey
	for _, k := range doc.Keys {
		pub, err := rsaPublicFromJWK(k)
		if err != nil {
			continue
		}
		if anyKey == nil {
			anyKey = pub
		}
		if kid := strings.TrimSpace(k.Kid); kid != "" {
			keys[kid] = pub
		}
	}
	if anyKey == nil && len(keys) == 0 {
		return nil, nil, errors.New("jwks has no usable RSA keys")
	}
	jwksCacheMu.Lock()
	jwksCache[jwksURL] = jwksCacheEntry{fetchedAt: time.Now(), keys: keys, anyKey: anyKey}
	jwksCacheMu.Unlock()
	return keys, anyKey, nil
}

// parseJWTClaimsWithJWKS 用 JWKS 校验 JWT，返回 claims map。
func parseJWTClaimsWithJWKS(cfg *store.SsoConfig, rawToken string) (map[string]any, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" || strings.Count(rawToken, ".") != 2 {
		return nil, errors.New("not a jwt")
	}
	jwksURL := cfg.JWKSURL()
	if jwksURL == "" {
		return nil, errors.New("jwks_uri not set")
	}
	keys, anyKey, err := loadJWKS(jwksURL)
	if err != nil {
		return nil, err
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}))
	tok, err := parser.Parse(rawToken, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid != "" {
			if k, ok := keys[kid]; ok {
				return k, nil
			}
			return nil, fmt.Errorf("unknown kid %s", kid)
		}
		if anyKey != nil {
			return anyKey, nil
		}
		return nil, errors.New("no jwk for token")
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("invalid jwt")
	}
	mc, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("jwt claims type")
	}
	out := map[string]any{}
	for k, v := range mc {
		out[k] = v
	}
	return out, nil
}
