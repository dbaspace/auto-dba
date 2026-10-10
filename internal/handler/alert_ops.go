package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

// Postmortem 生成复盘 Markdown。
func (h *AlertHandler) Postmortem(c *gin.Context) {
	var b struct {
		Since string `json:"since"`
		Until string `json:"until"`
	}
	_ = c.ShouldBindJSON(&b)
	if strings.TrimSpace(b.Since) == "" {
		b.Since = c.Query("since")
	}
	if strings.TrimSpace(b.Until) == "" {
		b.Until = c.Query("until")
	}
	md, err := h.Store.BuildAlertPostmortem(b.Since, b.Until)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"markdown": md}})
}

// --- Inhibitions ---

func (h *AlertHandler) ListInhibitions(c *gin.Context) {
	list, err := h.Store.ListAlertInhibitions()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *AlertHandler) CreateInhibition(c *gin.Context) {
	var b store.AlertInhibition
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	b.CreatedBy = middleware.Username(c)
	b.Enabled = true
	if err := h.Store.CreateAlertInhibition(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *AlertHandler) UpdateInhibition(c *gin.Context) {
	exist, err := h.Store.GetAlertInhibition(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "抑制规则不存在"})
		return
	}
	var b store.AlertInhibition
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	exist.Name, exist.SourceMatchers = b.Name, b.SourceMatchers
	exist.TargetMatchers, exist.EqualLabels = b.TargetMatchers, b.EqualLabels
	exist.Enabled = b.Enabled
	if err := h.Store.SaveAlertInhibition(exist); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *AlertHandler) DeleteInhibition(c *gin.Context) {
	if err := h.Store.DeleteAlertInhibition(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": true})
}

// --- Routes ---

func (h *AlertHandler) ListRoutes(c *gin.Context) {
	list, err := h.Store.ListAlertRoutes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *AlertHandler) CreateRoute(c *gin.Context) {
	var b store.AlertRoute
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	b.CreatedBy = middleware.Username(c)
	if len(b.Channels) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请至少选择一个渠道"})
		return
	}
	b.Enabled = true
	if err := h.Store.CreateAlertRoute(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *AlertHandler) UpdateRoute(c *gin.Context) {
	exist, err := h.Store.GetAlertRoute(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "路由不存在"})
		return
	}
	var b store.AlertRoute
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	exist.Name, exist.Matchers = b.Name, b.Matchers
	exist.Channels, exist.GroupBy = b.Channels, b.GroupBy
	exist.Continue, exist.Priority, exist.Enabled = b.Continue, b.Priority, b.Enabled
	if err := h.Store.SaveAlertRoute(exist); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *AlertHandler) DeleteRoute(c *gin.Context) {
	if err := h.Store.DeleteAlertRoute(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": true})
}

// --- Oncall ---

func (h *AlertHandler) ListOncall(c *gin.Context) {
	list, err := h.Store.ListAlertOncall()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *AlertHandler) CreateOncall(c *gin.Context) {
	var b store.AlertOncallSlot
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	b.CreatedBy = middleware.Username(c)
	b.Enabled = true
	if err := h.Store.CreateAlertOncall(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": b})
}

func (h *AlertHandler) UpdateOncall(c *gin.Context) {
	exist, err := h.Store.GetAlertOncall(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "值班槽不存在"})
		return
	}
	var b store.AlertOncallSlot
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	exist.Name, exist.Weekday = b.Name, b.Weekday
	exist.StartHM, exist.EndHM = b.StartHM, b.EndHM
	exist.Usernames, exist.Enabled = b.Usernames, b.Enabled
	if err := h.Store.SaveAlertOncall(exist); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": exist})
}

func (h *AlertHandler) DeleteOncall(c *gin.Context) {
	if err := h.Store.DeleteAlertOncall(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": true})
}
