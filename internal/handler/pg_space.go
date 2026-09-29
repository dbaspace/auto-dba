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
	port, _ := strconv.ParseInt(c.Query("port"), 10, 64)
	data, err := h.Svc.StartCollectJob(c.Request.Context(),
		c.Query("instance_id"), c.Query("host"), port, pgSpaceOperator(c))
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
	jobID := strings.TrimSpace(c.Query("job_id"))
	data, err := fn(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
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
