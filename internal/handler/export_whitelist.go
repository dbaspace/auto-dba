package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func (h *DatabaseHandler) CheckExportWhitelist(c *gin.Context) {
	ok, err := h.Store.ExportWhitelistAllows(middleware.Username(c), c.Query("database_id"), time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"allowed": ok})
}

func (h *DatabaseHandler) ListExportWhitelist(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可查看导出白名单") {
		return
	}
	list, err := h.Store.ListExportWhitelist()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *DatabaseHandler) CreateExportWhitelist(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可维护导出白名单") {
		return
	}
	it, ok := bindExportWhitelist(c)
	if !ok {
		return
	}
	if err := h.Store.CreateExportWhitelist(it); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DatabaseHandler) UpdateExportWhitelist(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可维护导出白名单") {
		return
	}
	it, ok := bindExportWhitelist(c)
	if !ok {
		return
	}
	it.ID = c.Param("id")
	if err := h.Store.UpdateExportWhitelist(it); err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{"message": "导出白名单不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

func (h *DatabaseHandler) DeleteExportWhitelist(c *gin.Context) {
	if !requireAdminViewer(h.Store, c, "仅管理员可维护导出白名单") {
		return
	}
	if err := h.Store.DeleteExportWhitelist(c.Param("id")); err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, gin.H{"message": "导出白名单不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// RecordQueryExport 白名单用户导出当前结果后记一条审计，不进入查询历史。
func (h *DatabaseHandler) RecordQueryExport(c *gin.Context) {
	var req struct {
		DatabaseID string `json:"database_id"`
		Format     string `json:"format"`
		RowCount   int    `json:"row_count"`
		Encrypted  bool   `json:"encrypted"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	switch format {
	case "csv", "json", "sql":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "格式仅支持 csv、json、sql"})
		return
	}
	ok, err := h.Store.ExportWhitelistAllows(middleware.Username(c), req.DatabaseID, time.Now().UTC())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"message": "不在导出白名单内"})
		return
	}
	target := strings.TrimSpace(req.DatabaseID)
	var inst *store.Instance
	if db, derr := h.Store.GetDatabase(req.DatabaseID); derr == nil && db != nil {
		if db.DBName != "" {
			target = db.DBName
		}
		inst, _ = h.Store.GetInstance(db.InstanceID)
	}
	msg := ""
	if req.Encrypted {
		msg = "password_zip"
	}
	if req.RowCount < 0 {
		req.RowCount = 0
	}
	it := &store.QueryAudit{
		Kind: "export", Target: target,
		Statement: "导出 " + format,
		Success:   true, Message: msg, RowCount: req.RowCount,
	}
	fillInstanceAudit(it, inst)
	recordQueryAudit(h.Store, c, it)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func bindExportWhitelist(c *gin.Context) (*store.ExportWhitelist, bool) {
	var req struct {
		Username string   `json:"username"`
		DBIDs    []string `json:"db_ids"`
		ExpireAt string   `json:"expire_at"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return nil, false
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请填写用户名"})
		return nil, false
	}
	ids := make([]string, 0, len(req.DBIDs))
	seen := map[string]bool{}
	for _, id := range req.DBIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请选择数据库"})
		return nil, false
	}
	expire := strings.TrimSpace(req.ExpireAt)
	if expire != "" {
		if _, err := time.Parse(time.RFC3339, expire); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "过期时间格式不正确"})
			return nil, false
		}
	}
	return &store.ExportWhitelist{Username: req.Username, DBIDs: ids, ExpireAt: expire}, true
}
