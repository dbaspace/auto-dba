package handler

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/ossupload"
)

// OSSUploadHandler 配置中心 · OSS 分片上传。
type OSSUploadHandler struct {
	Engine *ossupload.Engine
}

func (h *OSSUploadHandler) eng() *ossupload.Engine {
	return h.Engine
}

func (h *OSSUploadHandler) List(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := h.eng().Store.ListOSSUploadJobs(limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *OSSUploadHandler) Get(c *gin.Context) {
	j, err := h.eng().Store.GetOSSUploadJob(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if j == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "上传任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": j})
}

type createOSSUploadReq struct {
	ProfileID string `json:"profile_id"`
	FileName  string `json:"file_name"`
	FileSize  int64  `json:"file_size"`
	SHA256    string `json:"sha256"`
	ChunkSize int64  `json:"chunk_size"`
}

func (h *OSSUploadHandler) Create(c *gin.Context) {
	var req createOSSUploadReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	req.ProfileID = strings.TrimSpace(req.ProfileID)
	req.FileName = strings.TrimSpace(req.FileName)
	if req.ProfileID == "" || req.FileName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "profile_id / file_name 必填"})
		return
	}
	job, err := h.eng().InitUpload(c.Request.Context(), req.ProfileID, req.FileName, req.FileSize, req.SHA256, req.ChunkSize, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job})
}

func (h *OSSUploadHandler) PutPart(c *gin.Context) {
	jobID := c.Param("id")
	n, err := strconv.Atoi(c.Param("n"))
	if err != nil || n < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "part_number 无效"})
		return
	}
	size := c.Request.ContentLength
	if size < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "缺少 Content-Length"})
		return
	}
	body := io.LimitReader(c.Request.Body, size+1)
	job, err := h.eng().UploadPart(c.Request.Context(), jobID, n, body, size)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": job})
}

func (h *OSSUploadHandler) Complete(c *gin.Context) {
	if err := h.eng().EnqueueComplete(c.Param("id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	j, _ := h.eng().Store.GetOSSUploadJob(c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"data": j, "message": "已入队后台合并与校验"})
}

func (h *OSSUploadHandler) Abort(c *gin.Context) {
	j, err := h.eng().Abort(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": j})
}
