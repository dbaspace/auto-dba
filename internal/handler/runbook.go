package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/runbook"
)

type RunbookHandler struct {
	Svc *runbook.Service
}

// Preview POST /runbook/preview  body: {alert_id}
func (h *RunbookHandler) Preview(c *gin.Context) {
	if h == nil || h.Svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "runbook 未启用"})
		return
	}
	var body struct {
		AlertID string `json:"alert_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.AlertID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "alert_id 必填"})
		return
	}
	a, err := h.Svc.Store.GetAlertInTenant(body.AlertID, effectiveListTenantID(h.Svc.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "告警不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": h.Svc.Preview(a)})
}

// Execute POST /runbook/execute  body: {alert_id, action, confirm:true}
func (h *RunbookHandler) Execute(c *gin.Context) {
	if h == nil || h.Svc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "runbook 未启用"})
		return
	}
	var body struct {
		AlertID string `json:"alert_id"`
		Action  string `json:"action"`
		Confirm bool   `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if !body.Confirm {
		c.JSON(http.StatusBadRequest, gin.H{"message": "须 confirm=true 二次确认"})
		return
	}
	a, err := h.Svc.Store.GetAlertInTenant(body.AlertID, effectiveListTenantID(h.Svc.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if a == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "告警不存在"})
		return
	}
	res, err := h.Svc.Execute(a, body.Action, middleware.Username(c), effectiveListTenantID(h.Svc.Store, c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}

// RunbookPreviewOnAlert GET /alerts/:id/runbook
func (h *AlertHandler) RunbookPreview(c *gin.Context) {
	a := h.loadAlert(c)
	if a == nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": runbook.New(h.Store).Preview(a)})
}
