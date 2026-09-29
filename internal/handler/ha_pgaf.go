package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/ha"
)

// GetPGAFState 查询 pg_auto_failover formation 状态。
func (h *HAHandler) GetPGAFState(c *gin.Context) {
	cluster, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil || cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "集群不存在"})
		return
	}
	if !strings.EqualFold(cluster.Mode, "pg_autofailover") {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅 pg_autofailover 模式支持"})
		return
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先绑定控制器"})
		return
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "控制器不存在"})
		return
	}
	auth, err := h.controllerAuth(ctrl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	st, err := ha.PGAFAgentShowState(ctx, auth)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": st})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": st})
}
