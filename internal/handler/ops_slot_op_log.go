package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/store"
)

func (h *SlotHandler) persistSlotOpLog(tenantID, kind, line string) {
	if h == nil || h.Store == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "_"
	}
	_ = h.Store.AppendSyncSlotOpLog(tenantID, kind, line)
}

// OpLogs GET /sync-slots/logs — 持久化的采集/重启操作日志（分页，第 1 页为最新）。
func (h *SlotHandler) OpLogs(c *gin.Context) {
	tid := effectiveListTenantID(h.Store, c)
	kind := strings.TrimSpace(c.Query("kind"))
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	list, total, err := h.Store.ListSyncSlotOpLogs(tid, kind, pageSize, (page-1)*pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.SyncSlotOpLog{}
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func formatSlotOpTS() string {
	return time.Now().Format("15:04:05")
}

func formatSlotOpLine(prefix, msg string) string {
	return fmt.Sprintf("%s [%s] %s", formatSlotOpTS(), prefix, msg)
}
