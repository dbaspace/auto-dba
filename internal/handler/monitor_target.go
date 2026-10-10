package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type MonitorTargetHandler struct {
	Store *store.DB
}

func (h *MonitorTargetHandler) List(c *gin.Context) {
	list, err := h.Store.ListMonitorJobsInTenant(effectiveListTenantID(h.Store, c), c.Query("keyword"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.MonitorJob{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MonitorTargetHandler) Get(c *gin.Context) {
	it, err := h.Store.GetMonitorJobInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "监控任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *MonitorTargetHandler) Preview(c *gin.Context) {
	kind := c.Query("kind")
	if kind == "" {
		kind = c.Query("type")
	}
	items, err := h.Store.PreviewMonitorTargets(kind)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if items == nil {
		items = []store.MonitorPreviewItem{}
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "source": "local"})
}

func normalizeMonitorJobReq(it *store.MonitorJob) error {
	it.Name = strings.TrimSpace(it.Name)
	if it.Name == "" {
		return errBad("请输入监控任务名称")
	}
	if it.TargetGroups == nil {
		it.TargetGroups = []store.MonitorTargetGroup{}
	}
	return nil
}

func (h *MonitorTargetHandler) Create(c *gin.Context) {
	var it store.MonitorJob
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := normalizeMonitorJobReq(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	stampTenantID(c, func(tid string) { it.TenantID = tid })
	if err := h.Store.CreateMonitorJob(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *MonitorTargetHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetMonitorJobInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "监控任务不存在"})
		return
	}
	var it store.MonitorJob
	if err := c.ShouldBindJSON(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	it.ID = exist.ID
	if err := normalizeMonitorJobReq(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.UpdateMonitorJob(&it); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	full, _ := h.Store.GetMonitorJobInTenant(it.ID, effectiveListTenantID(h.Store, c))
	c.JSON(http.StatusOK, gin.H{"data": full})
}

func (h *MonitorTargetHandler) Delete(c *gin.Context) {
	if exist, err := h.Store.GetMonitorJobInTenant(c.Param("id"), effectiveListTenantID(h.Store, c)); err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "监控任务不存在"})
		return
	}
	if err := h.Store.DeleteMonitorJob(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func actorName(c *gin.Context) string {
	return strings.TrimSpace(middleware.Username(c))
}
