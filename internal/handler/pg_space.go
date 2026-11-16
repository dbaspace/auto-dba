package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/pgspace"
	"auto-dba/internal/store"
)

// PgSpaceHandler PostgreSQL 表空间/存储分析。
type PgSpaceHandler struct {
	Svc *pgspace.Service
}

func (h *PgSpaceHandler) Instances(c *gin.Context) {
	list, err := h.Svc.ListInstances(c.Request.Context(), c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if set := tenantInstanceIDSet(h.Svc.Store, c); set != nil {
		kept := list[:0]
		for _, it := range list {
			if set[it.InstanceID] {
				kept = append(kept, it)
			}
		}
		list = kept
	}
	if list == nil {
		list = []*pgspace.InstanceItem{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"instances": list}, "message": "ok"})
}

func (h *PgSpaceHandler) Realtime(c *gin.Context) {
	port, _ := strconv.ParseInt(c.Query("port"), 10, 64)
	topN, _ := strconv.Atoi(c.Query("top_n"))
	data, err := h.Svc.Realtime(c.Request.Context(),
		c.Query("instance_id"), c.Query("host"), port, c.Query("db_name"), topN)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *PgSpaceHandler) Databases(c *gin.Context) {
	port, _ := strconv.ParseInt(c.Query("port"), 10, 64)
	data, err := h.Svc.Databases(c.Request.Context(), c.Query("instance_id"), c.Query("host"), port)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *PgSpaceHandler) Snapshots(c *gin.Context) {
	port, _ := strconv.ParseInt(c.Query("port"), 10, 64)
	current, _ := strconv.Atoi(c.DefaultQuery("current", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	onlyLatest := c.Query("only_latest") == "true" || c.Query("only_latest") == "1"
	page, err := h.Svc.Snapshots(c.Request.Context(),
		c.Query("instance_id"), c.Query("host"), port,
		c.Query("object_type"), c.Query("db_name"), onlyLatest, current, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": page, "message": "ok"})
}

func (h *PgSpaceHandler) Trend(c *gin.Context) {
	objectType := strings.TrimSpace(c.Query("object_type"))
	objectName := strings.TrimSpace(c.Query("object_name"))
	if objectType == "" || objectName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "object_type and object_name required"})
		return
	}
	port, _ := strconv.ParseInt(c.Query("port"), 10, 64)
	var capacity *int64
	if v := c.Query("capacity_bytes"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			capacity = &n
		}
	}
	data, err := h.Svc.Trend(c.Request.Context(),
		c.Query("instance_id"), c.Query("host"), port,
		objectType, c.Query("db_name"), objectName,
		c.Query("start_time"), c.Query("end_time"), capacity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *PgSpaceHandler) Summary(c *gin.Context) {
	port, _ := strconv.ParseInt(c.Query("port"), 10, 64)
	topN, _ := strconv.Atoi(c.Query("top_n"))
	data, err := h.Svc.Summary(c.Request.Context(), c.Query("instance_id"), c.Query("host"), port, topN)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *PgSpaceHandler) Advice(c *gin.Context) {
	port, _ := strconv.ParseInt(c.Query("port"), 10, 64)
	data, err := h.Svc.AdviceReport(c.Request.Context(), c.Query("instance_id"), c.Query("host"), port)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *PgSpaceHandler) AdviceSummary(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	data, err := h.Svc.AdviceFleet(c.Request.Context(), c.Query("severity"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *PgSpaceHandler) Collect(c *gin.Context) {
	instanceID, host, port, dbScope := pgSpaceCollectArgs(c)
	data, err := h.Svc.StartCollectJob(c.Request.Context(),
		instanceID, host, port, pgSpaceOperator(c), dbScope)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *PgSpaceHandler) CollectStatus(c *gin.Context) {
	jobID := strings.TrimSpace(c.Query("job_id"))
	item, err := h.Svc.GetJob(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item, "message": "ok"})
}

func (h *PgSpaceHandler) CollectJobs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Svc.ListJobs(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []*store.SpaceCollectJob{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"list": list}, "message": "ok"})
}

func (h *PgSpaceHandler) CollectPause(c *gin.Context) {
	h.jobControl(c, h.Svc.PauseJob)
}

func (h *PgSpaceHandler) CollectResume(c *gin.Context) {
	h.jobControl(c, h.Svc.ResumeJob)
}

func (h *PgSpaceHandler) CollectCancel(c *gin.Context) {
	h.jobControl(c, h.Svc.CancelJob)
}

func (h *PgSpaceHandler) jobControl(c *gin.Context, fn func(context.Context, string) (*pgspace.JobControlData, error)) {
	jobID := pgSpaceJobID(c)
	data, err := fn(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

type pgSpaceCollectBody struct {
	InstanceID string `json:"instance_id"`
	Host       string `json:"host"`
	Port       int64  `json:"port"`
	DBScope    string `json:"db_scope"`
	DBName     string `json:"db_name"`
}

type pgSpaceJobBody struct {
	JobID string `json:"job_id"`
}

// pgSpaceCollectArgs 同时接受 URL 参数和 JSON。已有 query 调用仍优先。
func pgSpaceCollectArgs(c *gin.Context) (instanceID, host string, port int64, dbScope string) {
	var body pgSpaceCollectBody
	_ = c.ShouldBindJSON(&body)
	instanceID = firstNonEmpty(c.Query("instance_id"), body.InstanceID)
	host = firstNonEmpty(c.Query("host"), body.Host)
	port = body.Port
	if q := strings.TrimSpace(c.Query("port")); q != "" {
		if n, err := strconv.ParseInt(q, 10, 64); err == nil {
			port = n
		}
	}
	dbScope = firstNonEmpty(c.Query("db_scope"), c.Query("db_name"), body.DBScope, body.DBName)
	return instanceID, host, port, dbScope
}

func pgSpaceJobID(c *gin.Context) string {
	var body pgSpaceJobBody
	_ = c.ShouldBindJSON(&body)
	return firstNonEmpty(c.Query("job_id"), body.JobID)
}

func pgSpaceOperator(c *gin.Context) string {
	for _, hdr := range []string{"X-User", "X-Username", "X-User-Name"} {
		if v := c.GetHeader(hdr); v != "" {
			return v
		}
	}
	if u, ok := c.Get("username"); ok {
		if s, ok := u.(string); ok && s != "" {
			return s
		}
	}
	return "manual"
}
