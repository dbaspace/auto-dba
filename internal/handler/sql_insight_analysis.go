package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/sqlinsight"
)

// SqlInsightAnalysisHandler 智能分析 API。
type SqlInsightAnalysisHandler struct {
	Svc *sqlinsight.Service
}

func (h *SqlInsightAnalysisHandler) Catalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.Svc.Catalog(), "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) ListMetrics(c *gin.Context) {
	q := &sqlinsight.MetricQuery{}
	_ = c.ShouldBindQuery(q)
	data, err := h.Svc.ListMetrics(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data.List, "total": data.Pagination.Total, "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) CreateMetric(c *gin.Context) {
	var req sqlinsight.MetricCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	item, err := h.Svc.CreateMetric(c.Request.Context(), &req, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item, "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) UpdateMetric(c *gin.Context) {
	var req sqlinsight.MetricUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	item, err := h.Svc.UpdateMetric(c.Request.Context(), c.Param("id"), &req, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item, "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) DeleteMetric(c *gin.Context) {
	if err := h.Svc.DeleteMetric(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *SqlInsightAnalysisHandler) ListRules(c *gin.Context) {
	q := &sqlinsight.RuleQuery{}
	_ = c.ShouldBindQuery(q)
	data, err := h.Svc.ListRules(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data.List, "total": data.Pagination.Total, "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) CreateRule(c *gin.Context) {
	var req sqlinsight.RuleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	item, err := h.Svc.CreateRule(c.Request.Context(), &req, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item, "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) UpdateRule(c *gin.Context) {
	var req sqlinsight.RuleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	item, err := h.Svc.UpdateRule(c.Request.Context(), c.Param("id"), &req, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item, "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) DeleteRule(c *gin.Context) {
	if err := h.Svc.DeleteRule(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *SqlInsightAnalysisHandler) ListRuns(c *gin.Context) {
	q := &sqlinsight.RunQuery{}
	_ = c.ShouldBindQuery(q)
	data, err := h.Svc.ListRuns(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": data.List, "total": data.Pagination.Total,
		"current": data.Pagination.Current, "page_size": data.Pagination.PageSize, "message": "ok",
	})
}

func (h *SqlInsightAnalysisHandler) CreateRun(c *gin.Context) {
	var req sqlinsight.RunCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	item, err := h.Svc.CreateRun(c.Request.Context(), &req, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item, "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) GetRun(c *gin.Context) {
	item, err := h.Svc.GetRun(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": item, "message": "ok"})
}

func (h *SqlInsightAnalysisHandler) ListFindings(c *gin.Context) {
	q := &sqlinsight.FindingQuery{}
	_ = c.ShouldBindQuery(q)
	data, err := h.Svc.ListFindings(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": data.List, "total": data.Pagination.Total,
		"current": data.Pagination.Current, "page_size": data.Pagination.PageSize, "message": "ok",
	})
}

func (h *SqlInsightAnalysisHandler) Feedback(c *gin.Context) {
	var req sqlinsight.FeedbackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if err := h.Svc.FindingFeedback(c.Request.Context(), c.Param("id"), &req, middleware.Username(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *SqlInsightAnalysisHandler) Summary(c *gin.Context) {
	q := &sqlinsight.SummaryQuery{}
	_ = c.ShouldBindQuery(q)
	data, err := h.Svc.Summary(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}
