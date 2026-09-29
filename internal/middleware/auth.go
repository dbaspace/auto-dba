package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"auto-dba/internal/store"
)

const (
	CtxUserID   = "user_id"
	CtxUsername = "username"
	CtxRole     = "role"
)

type Claims struct {
	UserID   string `json:"uid"`
	Username string `json:"sub_name"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func SignToken(secret, userID, username, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	c := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
}

func Auth(secret string, db *store.DB, skipPrefixes ...string) gin.HandlerFunc {
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
		claims := &Claims{}
		tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
			return []byte(secret), nil
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

// RequireURN 校验当前用户是否拥有指定动作/页面 URN；任一角色为 admin 则放行。
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
		ok, err := db.UserHasURN(u.ID, urn)
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
