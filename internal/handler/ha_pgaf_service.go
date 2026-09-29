package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/ha"
)

// GetPGAFServiceStatus systemd is-active。
func (h *HAHandler) GetPGAFServiceStatus(c *gin.Context) {
	auth, code, msg := h.pgafServiceAuth(c)
	if msg != "" {
		c.JSON(code, gin.H{"message": msg})
		return
	}
	unit := strings.TrimSpace(c.Query("unit"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	out, err := ha.PGAFAgentServiceCtrl(ctx, auth, "status", unit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// PGAFServiceCtrl start|stop|restart。
func (h *HAHandler) PGAFServiceCtrl(c *gin.Context) {
	auth, code, msg := h.pgafServiceAuth(c)
	if msg != "" {
		c.JSON(code, gin.H{"message": msg})
		return
	}
	action := strings.ToLower(strings.TrimSpace(c.Param("action")))
	var body struct {
		Unit string `json:"unit"`
	}
	_ = c.ShouldBindJSON(&body)
	unit := strings.TrimSpace(body.Unit)
	if unit == "" {
		unit = strings.TrimSpace(c.Query("unit"))
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	out, err := ha.PGAFAgentServiceCtrl(ctx, auth, action, unit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error(), "data": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

func (h *HAHandler) pgafServiceAuth(c *gin.Context) (ha.ControllerAuth, int, string) {
	cluster, err := h.Store.GetHACluster(c.Param("id"))
	if err != nil || cluster == nil {
		return ha.ControllerAuth{}, http.StatusNotFound, "集群不存在"
	}
	if !strings.EqualFold(cluster.Mode, "pg_autofailover") {
		return ha.ControllerAuth{}, http.StatusBadRequest, "仅 pg_autofailover 模式支持"
	}
	if strings.TrimSpace(cluster.ControllerID) == "" {
		return ha.ControllerAuth{}, http.StatusBadRequest, "请先绑定控制器"
	}
	ctrl, err := h.Store.GetHAController(cluster.ControllerID)
	if err != nil || ctrl == nil {
		return ha.ControllerAuth{}, http.StatusBadRequest, "控制器不存在"
	}
	auth, aerr := h.controllerAuth(ctrl)
	if aerr != nil {
		return ha.ControllerAuth{}, http.StatusInternalServerError, aerr.Error()
	}
	return auth, 0, ""
}
