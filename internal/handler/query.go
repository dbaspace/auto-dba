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

const maxQueryStatements = 3

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
		tid := middleware.TenantID(c)
		uid := middleware.UserID(c)
		granted, _ = h.Store.HasResourceGrant(tid, uid, "database", rec.ID)
		if !granted {
			via, _ := h.Store.DatabaseVisibleViaInstanceAccess(tid, uid, rec)
			granted = via
		}
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

func (h *DatabaseHandler) auditSubmittedQuery(c *gin.Context, sql, target, msg string, inst *store.Instance, success bool, rows int, elapsed int64, reqID string) {
	if strings.TrimSpace(sql) == "" {
		return
	}
	it := &store.QueryAudit{
		Kind: "db", Target: target, Statement: sql,
		Success: success, Message: msg, RowCount: rows, ElapsedMS: elapsed,
		ClientRequestID: reqID,
	}
	fillInstanceAudit(it, inst)
	recordQueryAudit(h.Store, c, it)
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

// ListColumns GET /databases/:id/columns?table=
// 查询窗口补全用。先确认库可见，不支持的类型不连库。一张表只发一条字典查询。
func (h *DatabaseHandler) ListColumns(c *gin.Context) {
	raw := strings.TrimSpace(c.Query("table"))
	if raw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "表名不能为空"})
		return
	}
	schema, table := dbprobe.SplitHintTable(raw)
	if table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "表名不能为空"})
		return
	}
	rec, inst, password, err := h.resolveVisibleDB(c, c.Param("id"))
	if err != nil {
		writeQueryErr(c, err)
		return
	}
	if !dbprobe.HintColumnsSupported(inst.DBType) {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"schema": schema, "table": table, "columns": []dbprobe.HintColumn{},
		}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	outSchema, cols, err := dbprobe.ListHintColumns(ctx, inst, password, rec.DBName, schema, table)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if cols == nil {
		cols = []dbprobe.HintColumn{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"schema": outSchema, "table": table, "columns": cols,
	}})
}

type queryReq struct {
	DatabaseID      string `json:"database_id"`
	SQL             string `json:"sql"`
	Limit           int    `json:"limit"`
	ClientRequestID string `json:"client_request_id"`
}

func (h *DatabaseHandler) Query(c *gin.Context) {
	var req queryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	req.SQL = strings.TrimSpace(req.SQL)
	reqID := normalizeClientRequestID(req.ClientRequestID)
	stmts, err := dbprobe.SplitReadOnlyStatements(req.SQL, maxQueryStatements)
	if err != nil {
		h.auditSubmittedQuery(c, req.SQL, strings.TrimSpace(req.DatabaseID), err.Error(), nil, false, 0, 0, reqID)
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if middleware.IsPATAuth(c) {
		if _, msg, blocked := dbprobe.PATQueryBlocked(stmts); blocked {
			h.auditSubmittedQuery(c, req.SQL, strings.TrimSpace(req.DatabaseID), msg, nil, false, 0, 0, reqID)
			c.JSON(http.StatusBadRequest, gin.H{"message": msg})
			return
		}
		if idx, missing := dbprobe.MissingPATSelectLimit(stmts); missing {
			msg := dbprobe.PATSelectLimitMessage(idx, len(stmts))
			h.auditSubmittedQuery(c, req.SQL, strings.TrimSpace(req.DatabaseID), msg, nil, false, 0, 0, reqID)
			c.JSON(http.StatusBadRequest, gin.H{"message": msg})
			return
		}
	}
	rec, inst, password, err := h.resolveVisibleDB(c, strings.TrimSpace(req.DatabaseID))
	if err != nil {
		h.auditSubmittedQuery(c, req.SQL, strings.TrimSpace(req.DatabaseID), err.Error(), nil, false, 0, 0, reqID)
		writeQueryErr(c, err)
		return
	}
	defLimit, maxLimit := dbprobe.DefaultQueryLimit, dbprobe.MaxQueryLimit
	if h.Store != nil {
		defLimit = h.Store.QueryDefaultLimit()
		maxLimit = h.Store.QueryMaxLimit()
	}
	qctx := c.Request.Context()
	if middleware.IsPATAuth(c) {
		if dbprobe.ForcePATReadOnlySession(inst.DBType) {
			qctx = dbprobe.WithReadOnlySession(qctx)
		}
	} else if h.Store != nil && h.Store.HardenReadOnlySession() {
		qctx = dbprobe.WithReadOnlySession(qctx)
	}
	ctx, cancel := context.WithTimeout(qctx, 25*time.Second)
	defer cancel()

	if reqID != "" {
		rememberPendingQuery(reqID, pendingQuery{
			UserID:       middleware.UserID(c),
			Username:     middleware.Username(c),
			DatabaseID:   rec.ID,
			SQL:          req.SQL,
			Target:       rec.DBName,
			InstanceID:   inst.ID,
			InstanceName: instanceAuditName(inst),
			DBType:       inst.DBType,
		})
		defer forgetPendingQuery(reqID)
	}

	results := make([]*dbprobe.QueryResult, 0, len(stmts))
	totalRows := 0
	var totalMS int64
	for _, stmt := range stmts {
		ens := dbprobe.EnsureQueryLimit(stmt, defLimit, maxLimit)
		res, qerr := dbprobe.ExecQuery(ctx, inst, password, rec.DBName, ens.SQL, ens.AppliedLimit)
		if qerr != nil {
			msg := explainQueryErr(qerr)
			if respondInteractiveQueryErr(c, qerr) {
				h.auditSubmittedQuery(c, req.SQL, rec.DBName, "查询已终止", inst, false, 0, 0, reqID)
				return
			}
			h.auditSubmittedQuery(c, req.SQL, rec.DBName, msg, inst, false, 0, 0, reqID)
			writeQueryJSON(c, http.StatusBadRequest, gin.H{"message": msg})
			return
		}
		if res == nil {
			res = &dbprobe.QueryResult{Columns: []string{}, Rows: [][]any{}}
		}
		res.Limit = ens.MaxLimit
		res.AppliedLimit = ens.AppliedLimit
		res.ValidSQL = ens.SQL
		if len(res.Rows) >= ens.AppliedLimit {
			res.Truncated = true
		}
		// 查询结果列脱敏（全局参数配置的列名）
		applyQueryDataMask(h.Store, res)
		results = append(results, res)
		totalRows += len(res.Rows)
		totalMS += res.ElapsedMS
	}

	primary := results[0]
	h.auditSubmittedQuery(c, req.SQL, rec.DBName, "", inst, true, totalRows, totalMS, reqID)

	payload := gin.H{"data": primary}
	if len(results) > 1 {
		payload["results"] = results
	}
	writeQueryJSON(c, http.StatusOK, payload)
}

type tableInfoReq struct {
	DatabaseID string `json:"database_id"`
	SQL        string `json:"sql"`
}

// TableInfo 解析 SQL 中 FROM 后第一个表并返回字段列表与建表 SQL。
func (h *DatabaseHandler) TableInfo(c *gin.Context) {
	var req tableInfoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	schema, table, err := dbprobe.FirstTableFromSQL(req.SQL)
	if err != nil {
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
	info, err := dbprobe.DescribeTable(ctx, inst, password, rec.DBName, schema, table)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": info})
}
