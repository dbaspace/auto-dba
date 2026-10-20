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

// OpLogs GET /sync-slots/logs — 持久化的采集/重启操作日志。
func (h *SlotHandler) OpLogs(c *gin.Context) {
	tid := effectiveListTenantID(h.Store, c)
	kind := strings.TrimSpace(c.Query("kind"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Store.ListSyncSlotOpLogs(tid, kind, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.SyncSlotOpLog{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func formatSlotOpTS() string {
	return time.Now().Format("15:04:05")
}

func formatSlotOpLine(prefix, msg string) string {
	return fmt.Sprintf("%s [%s] %s", formatSlotOpTS(), prefix, msg)
}
