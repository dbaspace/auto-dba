package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"auto-dba/internal/config"
	"auto-dba/internal/sessionstore"
	"auto-dba/internal/store"
)

const (
	CtxUserID        = "user_id"
	CtxUsername      = "username"
	CtxRole          = "role"
	CtxTenantID      = "tenant_id"
	CtxTenantCode    = "tenant_code"
	CtxImpersonate   = "tenant_impersonate"
	CtxJTI           = "jti"
	CtxAuthKind      = "auth_kind"   // jwt | pat
	CtxTokenID       = "token_id"    // PAT id
	CtxClientType    = "client_type" // mcp | cli | local_ai | browser | machine
	CtxGrantID       = "grant_id"
	CtxPATScopes     = "pat_scopes"
	HeaderTenantID   = "X-Tenant-Id"
	HeaderClientType = "X-Client-Type"
)

// SessionGate 可选 Redis 会话校验；nil 或不启用时鉴权与改造前一致。
type SessionGate interface {
	Enabled() bool
	FailOpen() bool
	Valid(ctx context.Context, userID, jti string) (bool, error)
}

type Claims struct {
	UserID      string `json:"uid"`
	Username    string `json:"sub_name"`
	Role        string `json:"role"`
	TenantID    string `json:"tid,omitempty"`
	TenantCode  string `json:"tcode,omitempty"`
	Impersonate bool   `json:"imp,omitempty"`
	jwt.RegisteredClaims
}

func SignToken(secret, userID, username, role, tenantID, tenantCode string, ttl time.Duration) (string, error) {
	tok, _, err := SignSessionToken(secret, userID, username, role, tenantID, tenantCode, false, ttl)
	return tok, err
}

func SignTokenFull(secret, userID, username, role, tenantID, tenantCode string, impersonate bool, ttl time.Duration) (string, error) {
	tok, _, err := SignSessionToken(secret, userID, username, role, tenantID, tenantCode, impersonate, ttl)
	return tok, err
}

// SignSessionToken 签发带 jti 的 JWT，返回 token 与 jti。
func SignSessionToken(secret, userID, username, role, tenantID, tenantCode string, impersonate bool, ttl time.Duration) (token, jti string, err error) {
	now := time.Now()
	jti = uuid.NewString()
	c := Claims{
		UserID:      userID,
		Username:    username,
		Role:        role,
		TenantID:    strings.TrimSpace(tenantID),
		TenantCode:  strings.TrimSpace(tenantCode),
		Impersonate: impersonate,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
	return token, jti, err
}

// jwtRenewWithin 剩余不足该时长时，用原 jti 重签，把 exp 续到一个完整会话周期。
const jwtRenewWithin = 12 * time.Hour

// ResignSessionToken 沿用原 jti 重签。exp 为现在 + ttl，供有效期内换发。
func ResignSessionToken(secret string, src *Claims, ttl time.Duration) (string, error) {
	if strings.TrimSpace(secret) == "" || src == nil || strings.TrimSpace(src.ID) == "" || ttl <= 0 {
		return "", errors.New("session claims required")
	}
	now := time.Now()
	c := *src
	c.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	c.IssuedAt = jwt.NewNumericDate(now)
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
}

// stripBearer 去掉 Authorization 前缀。
func stripBearer(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
		return strings.TrimSpace(raw[7:])
	}
	return raw
}

// looksLikeJWT 粗判 JWT 外形（payload 以 eyJ 开头）。注意：Nacos accessToken 也可能是 eyJ…，不能单靠外形区分平台会话。
func looksLikeJWT(raw string) bool {
	return strings.HasPrefix(stripBearer(raw), "eyJ")
}

// isEmbedProxyPath 同源嵌入控制台代理：Authorization 常为第三方 JWT/token，平台会话在 Cookie。
func isEmbedProxyPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/nacos-proxy/") ||
		strings.HasPrefix(path, "/api/v1/ds-proxy/") ||
		strings.HasPrefix(path, "/api/v1/nacos-console/") ||
		strings.HasPrefix(path, "/api/v1/ds-console/")
}

// pickAuthCredential 选取平台鉴权凭据。
// nacos-proxy / ds-proxy：优先 Cookie（平台 JWT），因 Authorization 常为 Nacos/海豚签发的 eyJ token。
// 普通 API：优先像 JWT 的 Authorization，再 Cookie / query。
func pickAuthCredential(path, authHeader, cookie, query string) string {
	h := stripBearer(authHeader)
	ck := strings.TrimSpace(cookie)
	q := strings.TrimSpace(query)
	if isEmbedProxyPath(path) {
		if ck != "" {
			return ck
		}
		if q != "" {
			return q
		}
		return h
	}
	if h != "" && looksLikeJWT(h) {
		return h
	}
	if ck != "" && looksLikeJWT(ck) {
		return ck
	}
	if q != "" && looksLikeJWT(q) {
		return q
	}
	if h != "" {
		return h
	}
	if ck != "" {
		return ck
	}
	return q
}

// Auth 使用 config.CurrentJWT()（atomic，热更安全）；secret 参数保留兼容，优先 CurrentJWT。
// sessions 可为 nil：不启用 Redis 会话校验（默认，不影响现网）。
func Auth(secret *string, db *store.DB, sessions SessionGate, skipPrefixes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for _, p := range skipPrefixes {
			if path == p || strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}
		cookie := ""
		if ck, err := c.Cookie("token"); err == nil {
			cookie = ck
		}
		raw := pickAuthCredential(path, c.GetHeader("Authorization"), cookie, c.Query("access_token"))
		if raw == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized", "diagnostic_message": "authorization key is empty"})
			c.Abort()
			return
		}
		// 个人访问令牌（MCP/CLI/本地 AI）
		if db != nil && strings.HasPrefix(raw, store.PATPrefix) {
			pat, grant, u, perr := db.LookupValidClientPAT(raw)
			if perr != nil || pat == nil || u == nil || grant == nil {
				msg := "invalid personal access token"
				if perr != nil {
					msg = perr.Error()
				}
				c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized", "diagnostic_message": msg})
				c.Abort()
				return
			}
			tid := strings.TrimSpace(grant.TenantID)
			tcode := ""
			if tid == "" {
				if ts, _ := db.ListUserTenants(u.ID); len(ts) > 0 {
					tid = ts[0].ID
					tcode = ts[0].Code
				}
			} else if t, _ := db.GetTenant(tid); t != nil {
				tcode = t.Code
			}
			if tid == "" {
				c.JSON(http.StatusForbidden, gin.H{"message": "访问授权未绑定租户且用户无可用租户", "diagnostic_message": "pat tenant empty"})
				c.Abort()
				return
			}
			c.Set(CtxUserID, u.ID)
			c.Set(CtxUsername, u.Username)
			c.Set(CtxRole, u.Role)
			c.Set(CtxTenantID, tid)
			c.Set(CtxTenantCode, tcode)
			c.Set(CtxImpersonate, false)
			c.Set(CtxJTI, "")
			c.Set(CtxAuthKind, "pat")
			c.Set(CtxTokenID, pat.ID)
			c.Set(CtxGrantID, grant.ID)
			c.Set(CtxPATScopes, pat.Scopes)
			ct := strings.TrimSpace(pat.ClientType)
			if h := strings.TrimSpace(c.GetHeader(HeaderClientType)); h != "" {
				ct = h
			}
			if ct == "" {
				ct = "machine"
			}
			c.Set(CtxClientType, strings.ToLower(ct))
			c.Next()
			return
		}
		sec := config.CurrentJWT()
		if sec == "" && secret != nil {
			sec = *secret
		}
		claims := &Claims{}
		tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
			return []byte(sec), nil
		})
		if err != nil || !tok.Valid {
			if errors.Is(err, jwt.ErrTokenExpired) {
				c.JSON(http.StatusUnauthorized, gin.H{
					"message":            "登录已过期，请重新登录",
					"code":               "session_expired",
					"diagnostic_message": "token expired",
				})
				c.Abort()
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized", "diagnostic_message": "invalid token"})
			c.Abort()
			return
		}
		var pwdChanged string
		if db != nil && strings.TrimSpace(claims.UserID) != "" {
			u, uerr := db.GetUserByID(claims.UserID)
			if uerr == nil && u != nil {
				if u.IsLoginBlocked() {
					c.JSON(http.StatusUnauthorized, gin.H{"message": u.LoginBlockedMessage()})
					c.Abort()
					return
				}
				pwdChanged = u.PasswordChangedAt
			}
		}
		if tokenIssuedBeforePasswordChange(claims.IssuedAt, pwdChanged) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"message":            "会话已失效，请重新登录",
				"code":               "session_revoked",
				"diagnostic_message": "password changed",
			})
			c.Abort()
			return
		}
		jti := strings.TrimSpace(claims.ID)
		if sessions != nil && sessions.Enabled() && jti != "" && !sessions.FailOpen() {
			ok, verr := sessions.Valid(c.Request.Context(), claims.UserID, jti)
			if errors.Is(verr, sessionstore.ErrSessionExpired) {
				c.JSON(http.StatusUnauthorized, gin.H{
					"message":            "登录已过期，请重新登录",
					"code":               "session_expired",
					"diagnostic_message": "redis session expired",
				})
				c.Abort()
				return
			}
			if verr != nil {
				c.JSON(http.StatusUnauthorized, gin.H{
					"message": "会话服务暂不可用，请稍后重试", "code": "session_unavailable",
					"diagnostic_message": verr.Error(),
				})
				c.Abort()
				return
			}
			if !ok {
				c.JSON(http.StatusUnauthorized, gin.H{
					"message": "会话已失效，请重新登录", "code": "session_revoked",
					"diagnostic_message": "redis session missing",
				})
				c.Abort()
				return
			}
		}
		renewAccessToken(c, sec, claims, sessions)
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
		c.Set(CtxRole, claims.Role)
		c.Set(CtxTenantID, strings.TrimSpace(claims.TenantID))
		c.Set(CtxTenantCode, strings.TrimSpace(claims.TenantCode))
		c.Set(CtxImpersonate, claims.Impersonate)
		c.Set(CtxJTI, jti)
		c.Set(CtxAuthKind, "jwt")
		ct := strings.ToLower(strings.TrimSpace(c.GetHeader(HeaderClientType)))
		if ct == "" {
			ct = "browser"
		}
		c.Set(CtxClientType, ct)
		c.Next()
	}
}

type sessionTTLSource interface {
	SessionTTL() time.Duration
}

// renewAccessToken 在 JWT 剩余不足 12 小时时换发，响应头交给前端保存。jti 不变。
func renewAccessToken(c *gin.Context, secret string, claims *Claims, sessions SessionGate) {
	if claims == nil || claims.ExpiresAt == nil || strings.TrimSpace(secret) == "" || strings.TrimSpace(claims.ID) == "" {
		return
	}
	remain := time.Until(claims.ExpiresAt.Time)
	if remain <= 0 || remain >= jwtRenewWithin {
		return
	}
	ttl := 24 * time.Hour
	if src, ok := sessions.(sessionTTLSource); ok {
		if d := src.SessionTTL(); d > 0 {
			ttl = d
		}
	}
	tok, err := ResignSessionToken(secret, claims, ttl)
	if err != nil || tok == "" {
		return
	}
	c.Header("X-Access-Token", tok)
}

// tokenIssuedBeforePasswordChange 改密时间晚于 token 签发时间时，旧登录应失效。
// changedAt 为空表示从未改密。
func tokenIssuedBeforePasswordChange(issued *jwt.NumericDate, changedAt string) bool {
	changedAt = strings.TrimSpace(changedAt)
	if changedAt == "" {
		return false
	}
	changed, err := time.Parse(time.RFC3339, changedAt)
	if err != nil {
		return false
	}
	if issued == nil {
		return true
	}
	return issued.Time.Before(changed)
}

func JTI(c *gin.Context) string {
	v, _ := c.Get(CtxJTI)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func AuthKind(c *gin.Context) string {
	v, _ := c.Get(CtxAuthKind)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func TokenID(c *gin.Context) string {
	v, _ := c.Get(CtxTokenID)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func ClientType(c *gin.Context) string {
	v, _ := c.Get(CtxClientType)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func IsPATAuth(c *gin.Context) bool {
	return AuthKind(c) == "pat"
}

func Username(c *gin.Context) string {
	v, _ := c.Get(CtxUsername)
	s, _ := v.(string)
	return s
}

func UserID(c *gin.Context) string {
	v, _ := c.Get(CtxUserID)
	s, _ := v.(string)
	return s
}

func Role(c *gin.Context) string {
	v, _ := c.Get(CtxRole)
	s, _ := v.(string)
	return s
}

func TenantID(c *gin.Context) string {
	v, _ := c.Get(CtxTenantID)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func TenantCode(c *gin.Context) string {
	v, _ := c.Get(CtxTenantCode)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func IsImpersonating(c *gin.Context) bool {
	v, _ := c.Get(CtxImpersonate)
	b, _ := v.(bool)
	return b
}

// IsPrivileged 仅表示平台/租户内运维能力标签，不再用于绕过租户隔离。
func IsPrivileged(role string) bool {
	switch strings.ToLower(role) {
	case "admin", "platform_admin", "super_admin", "operator", "dba", "运维", "平台管理员":
		return true
	default:
		return false
	}
}

func IsAdminRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "platform_admin", "super_admin", "平台管理员":
		return true
	default:
		return false
	}
}

// TenantGuard 对业务 API 强制租户上下文；skipPrefixes 下的路径放行（登录选租户、平台租户治理等）。
func TenantGuard(db *store.DB, skipPrefixes ...string) gin.HandlerFunc {
	inner := TenantRequired(db)
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for _, p := range skipPrefixes {
			if path == p || strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}
		inner(c)
	}
}

// TenantRequired 要求 JWT 含 tid，且用户为该租户有效成员（或平台 impersonate）。
// X-Tenant-Id 仅校验一致性，不能覆盖 JWT.tid。
func TenantRequired(db *store.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		tid := TenantID(c)
		if tid == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"message":            "请先选择租户",
				"need_tenant_select": true,
				"diagnostic_message": "jwt tid is empty",
			})
			c.Abort()
			return
		}
		if hdr := strings.TrimSpace(c.GetHeader(HeaderTenantID)); hdr != "" && hdr != tid {
			c.JSON(http.StatusForbidden, gin.H{"message": "X-Tenant-Id 与登录租户不一致"})
			c.Abort()
			return
		}
		if db == nil {
			c.Next()
			return
		}
		uid := UserID(c)
		if IsImpersonating(c) {
			ok, err := db.UserIsPlatformAdmin(uid)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
				c.Abort()
				return
			}
			if !ok {
				c.JSON(http.StatusForbidden, gin.H{"message": "无效的租户审计会话"})
				c.Abort()
				return
			}
			t, err := db.GetTenant(tid)
			if err != nil || t == nil || t.Status != store.TenantStatusActive {
				c.JSON(http.StatusForbidden, gin.H{"message": "租户不可用"})
				c.Abort()
				return
			}
			c.Next()
			return
		}
		ok, err := db.IsActiveTenantMember(tid, uid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			c.Abort()
			return
		}
		if !ok {
			c.JSON(http.StatusForbidden, gin.H{"message": "无权访问该租户"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireURN 校验当前用户是否拥有指定动作/页面 URN（租户上下文优先）。
// 平台审计（impersonate）会话按全局角色鉴权，避免 admin 非目标租户成员时菜单/写接口全空。
func RequireURN(db *store.DB, urn string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if db == nil || strings.TrimSpace(urn) == "" {
			c.Next()
			return
		}
		u, err := db.GetUserByUsername(Username(c))
		if err != nil || u == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
			c.Abort()
			return
		}
		var ok bool
		if store.IsPlatformURN(urn) || IsImpersonating(c) {
			ok, err = db.UserHasURN(u.ID, urn)
		} else {
			ok, err = db.UserHasURNInTenant(u.ID, TenantID(c), urn)
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			c.Abort()
			return
		}
		if !ok {
			label := db.PermissionLabel(urn)
			c.JSON(http.StatusForbidden, gin.H{
				"message":      "无操作权限，请联系管理员开通「" + label + "」",
				"required_urn": urn,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
