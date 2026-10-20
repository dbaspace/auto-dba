package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/mysqldiag"
	"auto-dba/internal/pgdiag"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

func (h *SessionHandler) instanceID(c *gin.Context) string {
	id := strings.TrimSpace(c.Query("instance_id"))
	if id != "" {
		return id
	}
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	_ = c.ShouldBindJSON(&req)
	return strings.TrimSpace(req.InstanceID)
}

func (h *SessionHandler) loadPostgres(c *gin.Context) (*store.Instance, string, bool) {
	id := h.instanceID(c)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "instance_id 必填"})
		return nil, "", false
	}
	inst, err := resolveInstanceInTenant(h.Store, c, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return nil, "", false
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return nil, "", false
	}
	if !pgdiag.IsPostgres(inst.DBType) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持 PostgreSQL 实例"})
		return nil, "", false
	}
	if inst.Username == "" || inst.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例未配置账号密码"})
		return nil, "", false
	}
	plain, err := secret.Decrypt(h.Secret, inst.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "解密实例密码失败: " + err.Error()})
		return nil, "", false
	}
	return inst, plain, true
}

func (h *SessionHandler) Activities(c *gin.Context) {
	inst, password, ok := h.loadPostgres(c)
	if !ok {
		return
	}
	list, err := pgdiag.QueryActivities(c.Request.Context(), inst, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SessionHandler) Locks(c *gin.Context) {
	inst, password, ok := h.loadPostgres(c)
	if !ok {
		return
	}
	list, err := pgdiag.QueryLocks(c.Request.Context(), inst, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SessionHandler) Cancel(c *gin.Context) {
	h.cancelOrTerminate(c, false)
}

func (h *SessionHandler) Terminate(c *gin.Context) {
	h.cancelOrTerminate(c, true)
}

func (h *SessionHandler) cancelOrTerminate(c *gin.Context, terminate bool) {
	pid, err := pgdiag.ParsePID(c.Param("pid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	inst, password, ok := h.loadPostgres(c)
	if !ok {
		return
	}
	if terminate {
		err = pgdiag.TerminateBackend(c.Request.Context(), inst, password, pid)
	} else {
		err = pgdiag.CancelBackend(c.Request.Context(), inst, password, pid)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SessionHandler) loadMySQL(c *gin.Context) (*store.Instance, string, bool) {
	id := h.instanceID(c)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "instance_id 必填"})
		return nil, "", false
	}
	inst, err := resolveInstanceInTenant(h.Store, c, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return nil, "", false
	}
	if inst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "实例不存在"})
		return nil, "", false
	}
	if !mysqldiag.IsMySQL(inst.DBType) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅支持 MySQL/TiDB/OceanBase 实例"})
		return nil, "", false
	}
	if inst.Username == "" || inst.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "实例未配置账号密码"})
		return nil, "", false
	}
	plain, err := secret.Decrypt(h.Secret, inst.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "解密实例密码失败: " + err.Error()})
		return nil, "", false
	}
	return inst, plain, true
}

// MySQLProcesslist SHOW FULL PROCESSLIST。
func (h *SessionHandler) MySQLProcesslist(c *gin.Context) {
	inst, password, ok := h.loadMySQL(c)
	if !ok {
		return
	}
	list, err := mysqldiag.QueryProcesslist(c.Request.Context(), inst, password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// MySQLKill KILL <id>。
func (h *SessionHandler) MySQLKill(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "非法连接 ID"})
		return
	}
	inst, password, ok := h.loadMySQL(c)
	if !ok {
		return
	}
	if err := mysqldiag.Kill(c.Request.Context(), inst, password, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// MySQLDigests performance_schema Top SQL。
func (h *SessionHandler) MySQLDigests(c *gin.Context) {
	inst, password, ok := h.loadMySQL(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := mysqldiag.QueryDigests(c.Request.Context(), inst, password, limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}
