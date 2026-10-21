package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/copilot"
	"auto-dba/internal/middleware"
)

type CopilotHandler struct {
	Svc *copilot.Service
}

// Models GET /copilot/models
func (h *CopilotHandler) Models(c *gin.Context) {
	if h == nil || h.Svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "copilot 未启用"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.Svc.Models(c.Request.Context())})
}

// Chat POST /copilot/chat
func (h *CopilotHandler) Chat(c *gin.Context) {
	if h == nil || h.Svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "copilot 未启用"})
		return
	}
	var req copilot.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	req.TenantID = effectiveListTenantID(h.Svc.Store, c)
	req.Username = middleware.Username(c)
	if req.Stream {
		h.chatStream(c, req)
		return
	}
	resp, err := h.Svc.Chat(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": resp})
}

func (h *CopilotHandler) chatStream(c *gin.Context, req copilot.ChatRequest) {
	c.Writer.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	write := func(v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			return
		}
		_, _ = c.Writer.Write(append(raw, '\n'))
		if flusher != nil {
			flusher.Flush()
		}
	}
	req.OnStep = func(step copilot.TaskStep) {
		write(gin.H{"type": "step", "step": step})
	}
	resp, err := h.Svc.Chat(c.Request.Context(), req)
	if err != nil {
		write(gin.H{"type": "error", "message": err.Error()})
		return
	}
	write(gin.H{"type": "done", "data": resp})
}

// ListSessions GET /copilot/sessions?limit=50
func (h *CopilotHandler) ListSessions(c *gin.Context) {
	if h == nil || h.Svc == nil || h.Svc.Store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "copilot 未启用"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	tenant := effectiveListTenantID(h.Svc.Store, c)
	list, err := h.Svc.Store.ListCopilotSessions(tenant, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"items": list, "total": len(list)}})
}

// GetSessionTurns GET /copilot/sessions/:id/turns
func (h *CopilotHandler) GetSessionTurns(c *gin.Context) {
	if h == nil || h.Svc == nil || h.Svc.Store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "copilot 未启用"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "session id 必填"})
		return
	}
	turns, err := h.Svc.Store.ListCopilotTurns(id, 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	type turnDTO struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		Meta    string `json:"meta,omitempty"`
	}
	out := make([]turnDTO, 0, len(turns))
	for _, t := range turns {
		out = append(out, turnDTO{Role: t.Role, Content: t.Content})
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"session_id": id, "items": out, "total": len(out)}})
}

// CopilotChat 告警上下文快捷入口 POST /alerts/:id/copilot
func (h *AlertHandler) CopilotChat(c *gin.Context) {
	svc := copilot.New(h.Store)
	svc.Secret = h.Secret
	var req copilot.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if strings.TrimSpace(req.AlertID) == "" {
		req.AlertID = c.Param("id")
	}
	req.TenantID = effectiveListTenantID(h.Store, c)
	req.Username = middleware.Username(c)
	resp, err := svc.Chat(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": resp})
}
