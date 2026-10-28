package server

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// accessLogThrottle 对失败的终端 WebSocket 握手限频，并去掉 query 中的 token。
type accessLogThrottle struct {
	mu   sync.Mutex
	last map[string]throttleEntry
}

type throttleEntry struct {
	at    time.Time
	extra int // 自上次打印后被抑制的条数
}

func newAccessLogThrottle() *accessLogThrottle {
	return &accessLogThrottle{last: map[string]throttleEntry{}}
}

func (t *accessLogThrottle) allow(key string, every time.Duration) (ok bool, suppressed int) {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	e, hit := t.last[key]
	if hit && now.Sub(e.at) < every {
		e.extra++
		t.last[key] = e
		return false, 0
	}
	suppressed = e.extra
	t.last[key] = throttleEntry{at: now}
	if len(t.last) > 4096 {
		for k, v := range t.last {
			if now.Sub(v.at) > 10*every {
				delete(t.last, k)
			}
		}
	}
	return true, suppressed
}

func redactRequestPath(path string) string {
	if path == "" || !strings.Contains(path, "token=") {
		return path
	}
	u, err := url.Parse(path)
	if err != nil || u.RawQuery == "" {
		return stripTokenQuery(path)
	}
	q := u.Query()
	if q.Has("token") {
		q.Set("token", "[redacted]")
		u.RawQuery = q.Encode()
	}
	out := u.RequestURI()
	if out == "" {
		return u.Path
	}
	return out
}

func stripTokenQuery(path string) string {
	i := strings.Index(path, "?")
	if i < 0 {
		return path
	}
	base, raw := path[:i], path[i+1:]
	parts := strings.Split(raw, "&")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.HasPrefix(p, "token=") {
			kept = append(kept, "token=[redacted]")
			continue
		}
		kept = append(kept, p)
	}
	return base + "?" + strings.Join(kept, "&")
}

func isTerminalWSPath(path string) bool {
	p := path
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return p == "/api/v1/terminal/ws" || strings.HasSuffix(p, "/terminal/ws")
}

// accessLogger 替代 gin.Logger：脱敏 token；对 /terminal/ws 失败响应按客户端 IP 每分钟最多一条。
func accessLogger() gin.HandlerFunc {
	th := newAccessLogThrottle()
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		c.Next()
		status := c.Writer.Status()
		full := path
		if raw != "" {
			full = path + "?" + raw
		}
		redacted := redactRequestPath(full)
		if isTerminalWSPath(path) && status >= 400 {
			key := c.ClientIP() + "|" + path
			ok, suppressed := th.allow(key, time.Minute)
			if !ok {
				return
			}
			msg := fmt.Sprintf("[GIN] %s | %3d | %13v | %15s | %-7s %s",
				start.Format("2006/01/02 - 15:04:05"),
				status,
				time.Since(start),
				c.ClientIP(),
				c.Request.Method,
				redacted,
			)
			if suppressed > 0 {
				msg += fmt.Sprintf(" (suppressed %d similar)", suppressed)
			}
			fmt.Fprintln(gin.DefaultWriter, msg)
			return
		}
		fmt.Fprintf(gin.DefaultWriter, "[GIN] %s | %3d | %13v | %15s | %-7s %s\n",
			start.Format("2006/01/02 - 15:04:05"),
			status,
			time.Since(start),
			c.ClientIP(),
			c.Request.Method,
			redacted,
		)
	}
}
