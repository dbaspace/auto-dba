package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/redisrdb"
)

// RedisRDBHandler Redis RDB 内存分析。
type RedisRDBHandler struct {
	Svc *redisrdb.Service
}

func (h *RedisRDBHandler) Instances(c *gin.Context) {
	list, err := h.Svc.ListInstances(c.Request.Context(), c.Query("q"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []*redisrdb.InstanceItem{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"instances": list}, "message": "ok"})
}

func (h *RedisRDBHandler) Online(c *gin.Context) {
	id := strings.TrimSpace(c.Query("instance_id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "instance_id required"})
		return
	}
	data, err := h.Svc.OnlineSummary(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "message": "ok"})
}

func (h *RedisRDBHandler) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请上传 RDB 文件（字段名 file）"})
		return
	}
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer src.Close()

	job, err := h.Svc.CreateUploadJob(
		c.Request.Context(),
		c.PostForm("instance_id"),
		c.PostForm("note"),
		middleware.Username(c),
		file.Filename,
		file.Size,
		src,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job, "message": "ok"})
}

func (h *RedisRDBHandler) Agents(c *gin.Context) {
	list, err := h.Svc.ListAgents()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []*redisrdb.AgentItem{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"agents": list}, "message": "ok"})
}

func (h *RedisRDBHandler) Fetch(c *gin.Context) {
	var req struct {
		InstanceID string `json:"instance_id"`
		AgentID    string `json:"agent_id"`
		Note       string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "invalid json"})
		return
	}
	job, err := h.Svc.CreateAgentFetchJob(
		c.Request.Context(),
		req.InstanceID,
		req.AgentID,
		req.Note,
		middleware.Username(c),
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job, "message": "ok"})
}

func (h *RedisRDBHandler) Jobs(c *gin.Context) {
	list, err := h.Svc.ListJobs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []*redisrdb.Job{}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"jobs": list}, "message": "ok"})
}

func (h *RedisRDBHandler) Job(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	job, err := h.Svc.GetJob(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job, "message": "ok"})
}

func (h *RedisRDBHandler) Report(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	rep, err := h.Svc.GetReport(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": rep, "message": "ok"})
}

func (h *RedisRDBHandler) DeleteJob(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	if err := h.Svc.DeleteJob(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
