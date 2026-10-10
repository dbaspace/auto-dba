package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbprobe"
	"auto-dba/internal/middleware"
	"auto-dba/internal/secret"
	"auto-dba/internal/store"
)

var (
	errQueryNotFound   = errors.New("数据库不存在")
	errQueryForbidden  = errors.New("无权限访问该库")
	errQueryNoInstance = errors.New("所属实例不存在")
)

func (h *DatabaseHandler) resolveVisibleDB(c *gin.Context, id string) (*store.Database, *store.Instance, string, error) {
	var rec *store.Database
	var err error
	if platformAdminAuditing(h.Store, c) {
		rec, err = h.Store.GetDatabase(id)
	} else {
		rec, err = h.Store.GetDatabaseVisible(id, middleware.TenantID(c), middleware.UserID(c))
	}
	if err != nil {
		return nil, nil, "", err
	}
	if rec == nil {
		return nil, nil, "", errQueryNotFound
	}
	granted := false
	if !platformAdminAuditing(h.Store, c) {
		granted, _ = h.Store.HasResourceGrant(middleware.TenantID(c), middleware.UserID(c), "database", rec.ID)
	}
	if !granted {
		viewer := h.Store.ViewerOfInTenant(middleware.Username(c), middleware.Role(c), rec.TenantID)
		if !rec.VisibleTo(viewer) {
			return nil, nil, "", errQueryForbidden
		}
	}
	inst, err := resolveParentInstance(h.Store, c, rec)
	if err != nil {
		return nil, nil, "", err
	}
	if inst == nil {
		return nil, nil, "", errQueryNoInstance
	}
	password, err := secret.Decrypt(h.Secret, inst.Password)
	if err != nil {
		name := strings.TrimSpace(inst.Name)
		if name == "" {
			name = inst.ID
		}
		return nil, nil, "", fmt.Errorf(
			"实例「%s」密码解密失败（密钥可能已变更）。指定库租户不会改实例密码，请到资源配置→实例中重新填写密码后保存",
			name,
		)
	}
	return rec, inst, password, nil
}

func writeQueryErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errQueryNotFound), errors.Is(err, errQueryNoInstance):
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
	case errors.Is(err, errQueryForbidden):
		c.JSON(http.StatusForbidden, gin.H{"message": err.Error()})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
	}
}

func (h *DatabaseHandler) ListTables(c *gin.Context) {
	rec, inst, password, err := h.resolveVisibleDB(c, c.Param("id"))
	if err != nil {
		writeQueryErr(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	tables, err := dbprobe.ListTables(ctx, inst, password, rec.DBName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"data": []string{}, "message": err.Error()})
		return
	}
	if tables == nil {
		tables = []string{}
	}
	c.JSON(http.StatusOK, gin.H{"data": tables})
}

type queryReq struct {
	DatabaseID string `json:"database_id"`
	SQL        string `json:"sql"`
	Limit      int    `json:"limit"`
}

func (h *DatabaseHandler) Query(c *gin.Context) {
	var req queryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	req.SQL = strings.TrimSpace(req.SQL)
	if err := dbprobe.IsReadOnlySQL(req.SQL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	rec, inst, password, err := h.resolveVisibleDB(c, strings.TrimSpace(req.DatabaseID))
	if err != nil {
		writeQueryErr(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := dbprobe.ExecQuery(ctx, inst, password, rec.DBName, req.SQL, req.Limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": res})
}
