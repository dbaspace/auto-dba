package ssoauth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"auto-dba/internal/store"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

var (
	ErrUnsupportedGrant = errors.New("unsupported_grant_type")
)

type UserInfo struct {
	Username    string
	DisplayName string
	Email       string
	Subject     string // IdP sub，仅作关联，不当本地用户名
}

type authState struct {
	FrontendOrigin string
	RedirectPath   string
	CallbackURL    string // authorize 时使用的 redirect_uri，换票必须一致
	ExpiresAt      time.Time
}

type loginTicket struct {
	Token     string
	UserJSON  []byte
	ExpiresAt time.Time
}

var (
	stateMu  sync.Mutex
	states   = map[string]authState{}
	ticketMu sync.Mutex
	tickets  = map[string]loginTicket{}
)

func newOpaqueID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func PutState(frontendOrigin, redirectPath, callbackURL string) string {
	id := newOpaqueID()
	stateMu.Lock()
	defer stateMu.Unlock()
	states[id] = authState{
		FrontendOrigin: strings.TrimRight(strings.TrimSpace(frontendOrigin), "/"),
		RedirectPath:   strings.TrimSpace(redirectPath),
		CallbackURL:    strings.TrimSpace(callbackURL),
		ExpiresAt:      time.Now().Add(10 * time.Minute),
	}
	return id
}

func TakeState(id string) (frontendOrigin, redirectPath, callbackURL string, ok bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", "", false
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	st, exists := states[id]
	if !exists {
		return "", "", "", false
	}
	delete(states, id)
	if time.Now().After(st.ExpiresAt) {
		return "", "", "", false
	}
	return st.FrontendOrigin, st.RedirectPath, st.CallbackURL, true
}

func PutTicket(token string, userJSON []byte) string {
	id := newOpaqueID()
	ticketMu.Lock()
	defer ticketMu.Unlock()
	tickets[id] = loginTicket{
		Token:     token,
		UserJSON:  append([]byte(nil), userJSON...),
		ExpiresAt: time.Now().Add(2 * time.Minute),
	}
	return id
}

func TakeTicket(id string) (token string, userJSON []byte, ok bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil, false
	}
	ticketMu.Lock()
	defer ticketMu.Unlock()
	t, exists := tickets[id]
	if !exists {
		return "", nil, false
	}
	delete(tickets, id)
	if time.Now().After(t.ExpiresAt) {
		return "", nil, false
	}
	return t.Token, t.UserJSON, true
}

// BuildAuthorizeURL 构造授权码模式跳转地址。
func BuildAuthorizeURL(cfg *store.SsoConfig, state, redirectURI string) (string, error) {
	if cfg == nil {
		return "", errors.New("sso not configured")
	}
	authURL := cfg.AuthorizeURL()
	if authURL == "" {
		return "", errors.New("sso authorize_path not set")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return "", errors.New("sso client_id not set")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return "", errors.New("sso callback_url not set")
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	scope := strings.TrimSpace(cfg.Scope)
	if scope == "" && cfg.JWKSURL() != "" {
		scope = "openid profile"
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	sep := "?"
	if strings.Contains(authURL, "?") {
		sep = "&"
	}
	return authURL + sep + q.Encode(), nil
}

// ExchangeCode 用授权码换 access_token 并拉取 userinfo。
func ExchangeCode(cfg *store.SsoConfig, code, redirectURI string) (*UserInfo, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, errors.New("sso not configured")
	}
	tokenURL := cfg.TokenURL()
	if tokenURL == "" {
		return nil, errors.New("sso base_url / token_path not set")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("sso client_id not set")
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("missing authorization code")
	}
	if strings.TrimSpace(redirectURI) == "" {
		return nil, errors.New("sso callback_url not set")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sso token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tokenResp map[string]any
	_ = json.Unmarshal(body, &tokenResp)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, tokenError(resp.StatusCode, tokenResp, body)
	}
	accessToken, _ := tokenResp["access_token"].(string)
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("sso token response missing access_token")
	}
	return fetchUserInfo(cfg, accessToken, tokenResp, "")
}

// Verify 使用 OAuth2 Resource Owner Password Credentials 校验账号，并拉取 userinfo。
func Verify(cfg *store.SsoConfig, username, password string) (*UserInfo, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, errors.New("sso not configured")
	}
	tokenURL := cfg.TokenURL()
	if tokenURL == "" {
		return nil, errors.New("sso base_url / token_path not set")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("sso client_id not set")
	}
	if strings.TrimSpace(username) == "" || password == "" {
		return nil, errors.New("username/password required")
	}

	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", username)
	form.Set("password", password)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if scope := strings.TrimSpace(cfg.Scope); scope != "" {
		form.Set("scope", scope)
	}
	if cb := strings.TrimSpace(cfg.CallbackURL); cb != "" {
		form.Set("redirect_uri", cb)
	}

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sso token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tokenResp map[string]any
	_ = json.Unmarshal(body, &tokenResp)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, tokenError(resp.StatusCode, tokenResp, body)
	}
	accessToken, _ := tokenResp["access_token"].(string)
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("sso token response missing access_token")
	}
	return fetchUserInfo(cfg, accessToken, tokenResp, username)
}

func fetchUserInfo(cfg *store.SsoConfig, accessToken string, tokenResp map[string]any, fallbackUser string) (*UserInfo, error) {
	// 优先：配置了 jwks_uri 时用 id_token（或 JWT 形态的 access_token）取用户信息
	if cfg.JWKSURL() != "" {
		candidates := []string{}
		if idTok, _ := tokenResp["id_token"].(string); strings.TrimSpace(idTok) != "" {
			candidates = append(candidates, idTok)
		}
		if strings.Count(accessToken, ".") == 2 {
			candidates = append(candidates, accessToken)
		}
		for _, raw := range candidates {
			claims, err := parseJWTClaimsWithJWKS(cfg, raw)
			if err != nil {
				continue
			}
			u := userFromMap(cfg, claims, fallbackUser)
			if strings.TrimSpace(u.Username) != "" {
				return u, nil
			}
		}
	}

	infoURL := cfg.UserInfoURL()
	if infoURL == "" {
		return userFromMap(cfg, tokenResp, fallbackUser), nil
	}

	infoReq, err := http.NewRequest(http.MethodGet, infoURL, nil)
	if err != nil {
		return nil, err
	}
	infoReq.Header.Set("Authorization", "Bearer "+accessToken)
	infoReq.Header.Set("Accept", "application/json")
	infoResp, err := httpClient.Do(infoReq)
	if err != nil {
		return nil, fmt.Errorf("sso userinfo request failed: %w", err)
	}
	defer infoResp.Body.Close()
	infoBody, _ := io.ReadAll(io.LimitReader(infoResp.Body, 1<<20))
	if infoResp.StatusCode < 200 || infoResp.StatusCode >= 300 {
		var errMap map[string]any
		_ = json.Unmarshal(infoBody, &errMap)
		return nil, fmt.Errorf("sso userinfo failed: %s", pickErrMsg(errMap, infoBody))
	}
	var infoMap map[string]any
	if err := json.Unmarshal(infoBody, &infoMap); err != nil {
		return nil, fmt.Errorf("sso userinfo invalid json: %w", err)
	}
	return userFromMap(cfg, infoMap, fallbackUser), nil
}

// Ping 仅校验 token 端点可达（用错误凭据，期望 400/401 而非网络错误）。
func Ping(cfg *store.SsoConfig) error {
	if cfg == nil {
		return errors.New("sso not configured")
	}
	tokenURL := cfg.TokenURL()
	if tokenURL == "" {
		return errors.New("请填写 SSO 地址（Base URL）")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return errors.New("请填写 Client ID")
	}
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", "__sso_ping__")
	form.Set("password", "x")
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接 SSO: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	msg := strings.ToLower(pickErrMsg(m, body))
	if strings.Contains(msg, "unsupported_grant_type") {
		// Token 地址可达，但 IdP 不支持密码模式（常见，改走授权码即可）
		return nil
	}
	if resp.StatusCode == 404 {
		return fmt.Errorf("sso auth failed: 404 page not found")
	}
	// 其它 HTTP 响应也说明地址可达
	return nil
}

func IsUnsupportedGrant(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnsupportedGrant) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "unsupported_grant_type")
}

func tokenError(status int, m map[string]any, raw []byte) error {
	msg := pickErrMsg(m, raw)
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "unsupported_grant_type"):
		return fmt.Errorf("%w: IdP 不支持 password，请使用授权码登录", ErrUnsupportedGrant)
	case strings.Contains(lower, "invalid_grant"),
		strings.Contains(lower, "invalid credentials"),
		strings.Contains(lower, "bad credentials"),
		strings.Contains(lower, "用户名或密码"),
		strings.Contains(lower, "password"):
		return errors.New("invalid password")
	case strings.Contains(lower, "user not found"),
		strings.Contains(lower, "unknown user"),
		strings.Contains(lower, "用户不存在"):
		return errors.New("user not found")
	case status == 401 || status == 403:
		return errors.New("invalid password")
	default:
		if msg == "" {
			return fmt.Errorf("sso auth failed (HTTP %d)", status)
		}
		return fmt.Errorf("sso auth failed: %s", msg)
	}
}

func pickErrMsg(m map[string]any, raw []byte) string {
	if m != nil {
		for _, k := range []string{"error_description", "message", "msg", "error", "detail"} {
			if v, ok := m[k]; ok {
				if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
					return s
				}
			}
		}
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

func userFromMap(cfg *store.SsoConfig, m map[string]any, fallbackUser string) *UserInfo {
	u := &UserInfo{Username: fallbackUser}
	if m == nil {
		u.DisplayName = fallbackUser
		return u
	}
	claim := func(keys ...string) string {
		for _, k := range keys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if v, ok := m[k]; ok {
				s := strings.TrimSpace(fmt.Sprint(v))
				if s != "" && s != "<nil>" {
					return s
				}
			}
		}
		return ""
	}
	u.Subject = claim("sub")
	// 用户名：配置 Claim → nickname → 常见字段；sub 仅最后兜底
	unameClaim := ""
	if cfg != nil {
		unameClaim = cfg.UsernameClaim
	}
	if name := claim(unameClaim, "nickname", "preferred_username", "username", "user_name", "login", "sub"); name != "" {
		u.Username = name
	}
	dispClaim := ""
	if cfg != nil {
		dispClaim = cfg.DisplayNameClaim
	}
	u.DisplayName = claim(dispClaim, "name", "display_name", "nickname", "cn")
	if u.DisplayName == "" {
		u.DisplayName = u.Username
	}
	emailClaim := ""
	if cfg != nil {
		emailClaim = cfg.EmailClaim
	}
	u.Email = claim(emailClaim, "email", "mail")
	if u.Username == "" {
		u.Username = fallbackUser
	}
	return u
}
