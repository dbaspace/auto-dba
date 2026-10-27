package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// IdleTimeoutSource 提供登录闲置超时分钟数（每次请求读取，不缓存）。
type IdleTimeoutSource interface {
	LoginIdleTimeoutMin(def int) int
}

var idleMu sync.Mutex
var idleLastSeen = map[string]time.Time{}

// TouchIdle 记录用户最近活动时间（登录成功或鉴权通过时调用）。
func TouchIdle(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	idleMu.Lock()
	idleLastSeen[userID] = time.Now()
	idleMu.Unlock()
}

// ClearIdle 清除用户闲置记录（可选，登出时调用）。
func ClearIdle(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	idleMu.Lock()
	delete(idleLastSeen, userID)
	idleMu.Unlock()
}

// IdleSession 在 Auth 之后检查闲置超时：超时返回 401，否则刷新 lastSeen。
// 全局参数 login_idle_timeout_min：>0 启用，0 关闭；非法/空按 30 分钟。
func IdleSession(src IdleTimeoutSource) gin.HandlerFunc {
	return func(c *gin.Context) {
		// PAT 机器通道不受浏览器闲置超时影响
		if IsPATAuth(c) {
			c.Next()
			return
		}
		uid, _ := c.Get(CtxUserID)
		userID, _ := uid.(string)
		userID = strings.TrimSpace(userID)
		if userID == "" {
			c.Next()
			return
		}
		mins := 30
		if src != nil {
			mins = src.LoginIdleTimeoutMin(30)
		}
		if mins <= 0 {
			TouchIdle(userID)
			c.Next()
			return
		}
		now := time.Now()
		limit := time.Duration(mins) * time.Minute
		idleMu.Lock()
		last, ok := idleLastSeen[userID]
		if ok && now.Sub(last) > limit {
			delete(idleLastSeen, userID)
			idleMu.Unlock()
			c.JSON(http.StatusUnauthorized, gin.H{
				"message":            "登录已超时，请重新登录",
				"diagnostic_message": "idle timeout",
			})
			c.Abort()
			return
		}
		idleLastSeen[userID] = now
		idleMu.Unlock()
		c.Next()
	}
}
