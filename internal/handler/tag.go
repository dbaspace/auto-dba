package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/store"
)

type MonitorLabelTagHandler struct {
	Store *store.DB
}

func (h *MonitorLabelTagHandler) List(c *gin.Context) {
	list, err := h.Store.ListMonitorLabelTags()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.MonitorLabelTag{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MonitorLabelTagHandler) Create(c *gin.Context) {
	var t store.MonitorLabelTag
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Store.CreateMonitorLabelTag(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *MonitorLabelTagHandler) Update(c *gin.Context) {
	exist, err := h.Store.GetMonitorLabelTag(c.Param("id"))
	if err != nil || exist == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "标签不存在"})
		return
	}
	var t store.MonitorLabelTag
	if err := c.ShouldBindJSON(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	t.ID = exist.ID
	if err := h.Store.UpdateMonitorLabelTag(&t); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *MonitorLabelTagHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteMonitorLabelTag(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
