package handler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/dbmig"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type schemaCompareReq struct {
	LeftInstanceID  string   `json:"left_instance_id"`
	RightInstanceID string   `json:"right_instance_id"`
	LeftDB          string   `json:"left_db"`
	RightDB         string   `json:"right_db"`
	LeftSchema      string   `json:"left_schema"`
	RightSchema     string   `json:"right_schema"`
	Tables          []string `json:"tables"`
}

// SchemaCompare 兼容旧同步路径：改为创建异步任务并立即返回。
func (h *MigrationHandler) SchemaCompare(c *gin.Context) {
	h.SchemaCompareCreate(c)
}

func (h *MigrationHandler) SchemaCompareCreate(c *gin.Context) {
	var req schemaCompareReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请求体无效"})
		return
	}
	req.LeftInstanceID = strings.TrimSpace(req.LeftInstanceID)
	req.RightInstanceID = strings.TrimSpace(req.RightInstanceID)
	req.LeftDB = strings.TrimSpace(req.LeftDB)
	req.RightDB = strings.TrimSpace(req.RightDB)
	if req.LeftInstanceID == "" || req.RightInstanceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "left_instance_id / right_instance_id 必填"})
		return
	}
	if req.LeftDB == "" || req.RightDB == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "left_db / right_db 必填"})
		return
	}
	leftInst, err := h.Store.GetInstance(req.LeftInstanceID)
	if err != nil || leftInst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "左侧实例不存在"})
		return
	}
	rightInst, err := h.Store.GetInstance(req.RightInstanceID)
	if err != nil || rightInst == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "右侧实例不存在"})
		return
	}
	family, ok := dbmig.SameEngineFamily(leftInst.DBType, rightInst.DBType)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"message": "两侧数据库类型须同属一类（PostgreSQL 互比，或 MySQL/TiDB/OceanBase 互比）"})
		return
	}
	engine := "mysql"
	if family == "postgres" {
		engine = "postgres"
	}
	leftSchema := strings.TrimSpace(req.LeftSchema)
	rightSchema := strings.TrimSpace(req.RightSchema)
	if engine == "postgres" {
		if leftSchema == "" {
			leftSchema = "public"
		}
		if rightSchema == "" {
			rightSchema = "public"
		}
	}
	t := &store.SchemaCompareTask{
		Status:          store.StatusPending,
		LeftInstanceID:  req.LeftInstanceID,
		RightInstanceID: req.RightInstanceID,
		LeftDB:          req.LeftDB,
		RightDB:         req.RightDB,
		LeftSchema:      leftSchema,
		RightSchema:     rightSchema,
		Tables:          req.Tables,
		Engine:          engine,
		CreatedBy:       middleware.Username(c),
		ProgressMessage: "排队中",
	}
	if err := h.Store.CreateSchemaCompareTask(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	go dbmig.RunSchemaCompareTask(h.Store, h.Secret, t.ID)
	t.LeftInstanceName = leftInst.Name
	t.RightInstanceName = rightInst.Name
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": t, "message": "任务已创建"})
}

func (h *MigrationHandler) SchemaCompareList(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Store.ListSchemaCompareTasks(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.SchemaCompareTask{}
	}
	for i := range list {
		h.enrichSchemaCompareTask(&list[i])
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *MigrationHandler) SchemaCompareGet(c *gin.Context) {
	t, err := h.Store.GetSchemaCompareTask(c.Param("id"), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	h.enrichSchemaCompareTask(t)
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *MigrationHandler) SchemaCompareDelete(c *gin.Context) {
	t, err := h.Store.GetSchemaCompareTask(c.Param("id"), false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	if t.Status == store.StatusPending || t.Status == store.StatusRunning {
		c.JSON(http.StatusBadRequest, gin.H{"message": "任务执行中，无法删除"})
		return
	}
	if err := h.Store.DeleteSchemaCompareTask(t.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *MigrationHandler) SchemaCompareExport(c *gin.Context) {
	t, err := h.Store.GetSchemaCompareTask(c.Param("id"), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	if t.Status != store.StatusCompleted {
		c.JSON(http.StatusBadRequest, gin.H{"message": "仅已完成任务可导出"})
		return
	}
	var items []dbmig.TableSchemaDiff
	if t.ResultJSON != "" {
		_ = json.Unmarshal([]byte(t.ResultJSON), &items)
	}
	format := strings.ToLower(strings.TrimSpace(c.Query("format")))
	if format == "" {
		format = "csv"
	}
	name := "schema-compare-" + t.ID[:8]
	switch format {
	case "sql":
		sqlText := t.FixSQL
		if strings.TrimSpace(sqlText) == "" {
			sqlText = "-- 无修复 SQL\n"
		}
		c.Header("Content-Disposition", `attachment; filename="`+name+`.sql"; filename*=UTF-8''`+url.QueryEscape(name+".sql"))
		c.Data(http.StatusOK, "application/sql; charset=utf-8", []byte(sqlText))
	case "json":
		payload := gin.H{"summary": t.Summary, "items": items, "fix_sql": t.FixSQL}
		b, _ := json.MarshalIndent(payload, "", "  ")
		c.Header("Content-Disposition", `attachment; filename="`+name+`.json"; filename*=UTF-8''`+url.QueryEscape(name+".json"))
		c.Data(http.StatusOK, "application/json; charset=utf-8", b)
	default:
		b := dbmig.SchemaCompareExportCSV(items)
		c.Header("Content-Disposition", `attachment; filename="`+name+`.csv"; filename*=UTF-8''`+url.QueryEscape(name+".csv"))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", b)
	}
}

func (h *MigrationHandler) enrichSchemaCompareTask(t *store.SchemaCompareTask) {
	if t == nil {
		return
	}
	if left, _ := h.Store.GetInstance(t.LeftInstanceID); left != nil {
		t.LeftInstanceName = left.Name
	}
	if right, _ := h.Store.GetInstance(t.RightInstanceID); right != nil {
		t.RightInstanceName = right.Name
	}
}
