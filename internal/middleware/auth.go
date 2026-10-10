package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"auto-dba/internal/config"
	"auto-dba/internal/store"
)

const (
	CtxUserID       = "user_id"
	CtxUsername     = "username"
	CtxRole         = "role"
	CtxTenantID     = "tenant_id"
	CtxTenantCode   = "tenant_code"
	CtxImpersonate  = "tenant_impersonate"
	HeaderTenantID  = "X-Tenant-Id"
)

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
	return SignTokenFull(secret, userID, username, role, tenantID, tenantCode, false, ttl)
}

func SignTokenFull(secret, userID, username, role, tenantID, tenantCode string, impersonate bool, ttl time.Duration) (string, error) {
	now := time.Now()
	c := Claims{
		UserID:      userID,
		Username:    username,
		Role:        role,
		TenantID:    strings.TrimSpace(tenantID),
		TenantCode:  strings.TrimSpace(tenantCode),
		Impersonate: impersonate,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
}

// Auth 使用 config.CurrentJWT()（atomic，热更安全）；secret 参数保留兼容，优先 CurrentJWT。
func Auth(secret *string, db *store.DB, skipPrefixes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		for _, p := range skipPrefixes {
			if path == p || strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}
		raw := c.GetHeader("Authorization")
		if raw == "" {
			if ck, err := c.Cookie("token"); err == nil {
				raw = ck
			}
		}
		if raw == "" {
			raw = c.Query("access_token")
		}
		raw = strings.TrimSpace(raw)
		if strings.HasPrefix(strings.ToLower(raw), "bearer ") {
			raw = strings.TrimSpace(raw[7:])
		}
		if raw == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized", "diagnostic_message": "authorization key is empty"})
			c.Abort()
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
			c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized", "diagnostic_message": "invalid token"})
			c.Abort()
			return
		}
		if db != nil && strings.TrimSpace(claims.UserID) != "" {
			u, uerr := db.GetUserByID(claims.UserID)
			if uerr == nil && u != nil && u.IsLoginBlocked() {
				c.JSON(http.StatusUnauthorized, gin.H{"message": u.LoginBlockedMessage()})
				c.Abort()
				return
			}
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
		c.Set(CtxRole, claims.Role)
		c.Set(CtxTenantID, strings.TrimSpace(claims.TenantID))
		c.Set(CtxTenantCode, strings.TrimSpace(claims.TenantCode))
		c.Set(CtxImpersonate, claims.Impersonate)
		c.Next()
	}
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
				"message":             "请先选择租户",
				"need_tenant_select":  true,
				"diagnostic_message":  "jwt tid is empty",
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
