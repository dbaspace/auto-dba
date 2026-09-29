package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/pgdiag"
	"auto-dba/internal/store"
)

func parsePage(c *gin.Context) (current, pageSize, offset int) {
	current, _ = strconv.Atoi(c.DefaultQuery("current", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if current < 1 {
		current = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset = (current - 1) * pageSize
	return current, pageSize, offset
}

func (h *InsightHandler) ListSnapshots(c *gin.Context) {
	current, pageSize, offset := parsePage(c)
	list, total, err := h.Store.ListPGActivitySnapshots(store.PGActivitySnapshotFilter{
		InstanceID: c.Query("instance_id"),
		StartTime:  c.Query("start_time"),
		EndTime:    c.Query("end_time"),
		Offset:     offset,
		Limit:      pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      list,
		"total":     total,
		"current":   current,
		"page_size": pageSize,
	})
}

func (h *InsightHandler) SnapshotStats(c *gin.Context) {
	current, pageSize, offset := parsePage(c)
	list, total, err := h.Store.ListPGActivityStats(store.PGActivityStatFilter{
		InstanceID: c.Query("instance_id"),
		StartTime:  c.Query("start_time"),
		EndTime:    c.Query("end_time"),
		PDb:        c.Query("p_db"),
		PUser:      c.Query("p_user"),
		OrderBy:    c.Query("order_by"),
		Offset:     offset,
		Limit:      pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      list,
		"total":     total,
		"current":   current,
		"page_size": pageSize,
	})
}

func (h *InsightHandler) SnapshotSummary(c *gin.Context) {
	start := strings.TrimSpace(c.Query("start_time"))
	end := strings.TrimSpace(c.Query("end_time"))
	if start == "" || end == "" {
		now := time.Now().UTC()
		if end == "" {
			end = now.Format(time.RFC3339)
		}
		if start == "" {
			start = now.Add(-24 * time.Hour).Format(time.RFC3339)
		}
	}
	topN, _ := strconv.Atoi(c.DefaultQuery("top_n", "10"))
	if topN <= 0 {
		topN = 10
	}
	if topN > 50 {
		topN = 50
	}

	sum, err := h.Store.SummaryPGActivity(store.PGActivitySummaryFilter{
		InstanceID: c.Query("instance_id"),
		StartTime:  start,
		EndTime:    end,
		PDb:        c.Query("p_db"),
		PUser:      c.Query("p_user"),
		TopN:       topN,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"time_range": gin.H{
				"start": start,
				"end":   end,
			},
			"state_distribution": sum.StateDistribution,
			"duration_buckets":   sum.DurationBuckets,
			"top_databases":      sum.TopDatabases,
			"top_users":          sum.TopUsers,
			"top_instances":      sum.TopInstances,
			"trend":              sum.Trend,
		},
	})
}

func (h *InsightHandler) CollectSnapshots(c *gin.Context) {
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	_ = c.ShouldBindJSON(&req)
	if req.InstanceID == "" {
		req.InstanceID = c.Query("instance_id")
	}
	res, warns, err := pgdiag.Collect(c.Request.Context(), h.Store, h.Secret, strings.TrimSpace(req.InstanceID))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":                  true,
		"instances_processed": res.InstancesProcessed,
		"rows_written":        res.RowsWritten,
		"warnings":            warns,
	})
}
