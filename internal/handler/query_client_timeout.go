package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

const (
	queryTimeoutMessage = "查询超过 18 秒已中断"
	queryNetworkMessage = "网络较慢，结果在 90 秒内没有传完"
	queryBoolHint       = "该列是布尔类型，请写成 true 或 false，不要写 0 或 1"
)

type pendingQuery struct {
	UserID       string
	Username     string
	DatabaseID   string
	SQL          string
	Target       string
	InstanceID   string
	InstanceName string
	DBType       string
}

var pendingQueries sync.Map

func rememberPendingQuery(id string, p pendingQuery) {
	pendingQueries.Store(id, p)
}

func forgetPendingQuery(id string) {
	pendingQueries.Delete(id)
}

func loadPendingQuery(id string) (pendingQuery, bool) {
	v, ok := pendingQueries.Load(id)
	if !ok {
		return pendingQuery{}, false
	}
	p, ok := v.(pendingQuery)
	return p, ok
}

func normalizeClientRequestID(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 8 || len(s) > 64 {
		return ""
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return ""
		}
	}
	return s
}

func instanceAuditName(inst *store.Instance) string {
	if inst == nil {
		return ""
	}
	name := strings.TrimSpace(inst.Name)
	if name == "" {
		name = strings.TrimSpace(inst.DomainName)
	}
	if name == "" {
		name = strings.TrimSpace(inst.Host)
	}
	return name
}

func explainQueryErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	if errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(low, "canceling statement due to statement timeout") ||
		strings.Contains(low, "context deadline exceeded") {
		return queryTimeoutMessage
	}
	if strings.Contains(low, "operator does not exist: boolean = integer") {
		return msg + "。" + queryBoolHint
	}
	return msg
}

func writeQueryJSON(c *gin.Context, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") && len(body) >= 256 {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, werr := zw.Write(body); werr == nil && zw.Close() == nil {
			c.Header("Content-Encoding", "gzip")
			c.Header("Vary", "Accept-Encoding")
			c.Data(status, "application/json; charset=utf-8", buf.Bytes())
			return
		}
	}
	c.Data(status, "application/json; charset=utf-8", body)
}

// QueryClientTimeout 浏览器等待超时后补记审计。说明由服务端写死，不接受客户端传入的 SQL。
func (h *DatabaseHandler) QueryClientTimeout(c *gin.Context) {
	var req struct {
		ClientRequestID string `json:"client_request_id"`
		ElapsedMS       int64  `json:"elapsed_ms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	id := normalizeClientRequestID(req.ClientRequestID)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "client_request_id 无效"})
		return
	}
	elapsed := req.ElapsedMS
	if elapsed < 0 || elapsed > 10*60*1000 {
		elapsed = 90 * 1000
	}
	uid := middleware.UserID(c)
	username := middleware.Username(c)
	if h.Store != nil {
		if n, err := h.Store.MarkQueryClientTimeout(uid, username, id, queryNetworkMessage, elapsed); err == nil && n > 0 {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
	}
	p, ok := loadPendingQuery(id)
	if !ok || !sameQueryUser(p, uid, username) {
		c.JSON(http.StatusOK, gin.H{"ok": false})
		return
	}
	if _, _, _, err := h.resolveVisibleDB(c, p.DatabaseID); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false})
		return
	}
	it := &store.QueryAudit{
		Kind: "db", UserID: p.UserID, Username: p.Username,
		Target: p.Target, Statement: p.SQL, Success: false,
		Message: queryNetworkMessage, ElapsedMS: elapsed, ClientRequestID: id,
		InstanceID: p.InstanceID, InstanceName: p.InstanceName, DBType: p.DBType,
	}
	recordQueryAudit(h.Store, c, it)
	forgetPendingQuery(id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func sameQueryUser(p pendingQuery, uid, username string) bool {
	if uid != "" && p.UserID != "" && p.UserID == uid {
		return true
	}
	return username != "" && p.Username != "" && p.Username == username
}
