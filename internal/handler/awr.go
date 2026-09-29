package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/awr"
)

// AWRHandler 每日数据库 AWR 报告。
type AWRHandler struct {
	Svc *awr.Service
}

func (h *AWRHandler) List(c *gin.Context) {
	req := &awr.ReportQuery{}
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	data, err := h.Svc.ListReports(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *AWRHandler) Detail(c *gin.Context) {
	req := &awr.ReportDetailQuery{}
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	data, err := h.Svc.DetailReport(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *AWRHandler) Export(c *gin.Context) {
	req := &awr.ReportDetailQuery{}
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	html, filename, err := h.Svc.ExportHTML(c.Request.Context(), req, c.Query("format"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func (h *AWRHandler) Summary(c *gin.Context) {
	req := &awr.SummaryQuery{}
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	data, err := h.Svc.SummaryReport(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *AWRHandler) SummaryExport(c *gin.Context) {
	req := &awr.SummaryQuery{}
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	html, filename, err := h.Svc.ExportSummaryHTML(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

func (h *AWRHandler) Generate(c *gin.Context) {
	req := &awr.GenerateReq{}
	_ = c.ShouldBindJSON(req)
	data, err := h.Svc.StartGenerateJob(c.Request.Context(), req, awrOperator(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *AWRHandler) GenerateStatus(c *gin.Context) {
	jobID := strings.TrimSpace(c.Query("job_id"))
	item, err := h.Svc.GetGenerateJob(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item, "message": "ok"})
}

func (h *AWRHandler) GenerateJobs(c *gin.Context) {
	req := &awr.GenerateJobQuery{}
	_ = c.ShouldBindQuery(req)
	list, err := h.Svc.ListGenerateJobs(c.Request.Context(), req.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []*awr.GenerateJobItem{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"list": list}, "message": "ok"})
}

func (h *AWRHandler) GeneratePause(c *gin.Context) {
	h.jobControl(c, h.Svc.PauseGenerateJob)
}

func (h *AWRHandler) GenerateResume(c *gin.Context) {
	h.jobControl(c, h.Svc.ResumeGenerateJob)
}

func (h *AWRHandler) GenerateCancel(c *gin.Context) {
	h.jobControl(c, h.Svc.CancelGenerateJob)
}

func (h *AWRHandler) jobControl(c *gin.Context, fn func(context.Context, string) (*awr.JobControlData, error)) {
	jobID := strings.TrimSpace(c.Query("job_id"))
	data, err := fn(c.Request.Context(), jobID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func awrOperator(c *gin.Context) string {
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
