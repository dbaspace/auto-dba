package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/flashback"
	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type FlashbackHandler struct {
	Store  *store.DB
	Secret string
	Cfg    *config.Config
	Engine *flashback.FlashbackImpl
}

func (h *FlashbackHandler) engine() *flashback.FlashbackImpl {
	if h.Engine == nil {
		h.Engine = flashback.New()
	}
	return h.Engine
}

func (h *FlashbackHandler) List(c *gin.Context) {
	list, err := h.Store.ListFlashbacksInTenant(effectiveListTenantID(h.Store, c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if list == nil {
		list = []store.FlashbackTask{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *FlashbackHandler) Get(c *gin.Context) {
	t, err := h.Store.GetFlashbackInTenant(c.Param("id"), effectiveListTenantID(h.Store, c))
	if err != nil || t == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *FlashbackHandler) Precheck(c *gin.Context) {
	var req flashback.FlashbackTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": "参数无效"})
		return
	}
	out, err := h.engine().Precheck(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	for i := range out.Items {
		out.Items[i].OK = out.Items[i].Status == "passed" || out.Items[i].Status == "warning"
	}
	c.JSON(http.StatusOK, out)
}

func (h *FlashbackHandler) Selftest(c *gin.Context) {
	var req flashback.FlashbackSelftestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": "参数无效：instance_id 与 database 必填"})
		return
	}
	out, err := h.engine().Selftest(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *FlashbackHandler) Create(c *gin.Context) {
	var req flashback.FlashbackTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	preferred := strings.TrimSpace(req.Reviewer)
	if preferred == "" {
		preferred = strings.TrimSpace(req.Approver)
	}
	if preferred == "" {
		preferred = strings.TrimSpace(req.AssignedTo)
	}
	reviewer, aerr := resolveTenantAssignee(h.Store, middleware.TenantID(c), preferred)
	if aerr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": aerr.Error()})
		return
	}
	dbName := strings.TrimSpace(req.Database)
	if dbName == "" {
		dbName = strings.TrimSpace(req.DBName)
	}
	th := &TicketHandler{Store: h.Store}
	if approvers := th.resolveApprovers(req.InstanceID, dbName); len(approvers) > 0 && !usernameInList(reviewer, approvers) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "审核人不在实例/库审批人名单中"})
		return
	}
	req.Reviewer = reviewer
	t, err := h.engine().Create(c.Request.Context(), &req, middleware.Username(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": t})
}

func (h *FlashbackHandler) Logs(c *gin.Context) {
	list, err := h.Store.ListFlashbackLogs(c.Param("id"), 200)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *FlashbackHandler) SQLs(c *gin.Context) {
	kind := c.Query("kind")
	var ops []string
	if raw := strings.TrimSpace(c.Query("ops")); raw != "" {
		ops = strings.Split(raw, ",")
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	list, total, err := h.Store.ListFlashbackSQLs(c.Param("id"), kind, ops, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "total": total})
}

func (h *FlashbackHandler) GetCloudSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"data": h.Store.GetFlashbackCloudSettings()})
}

func (h *FlashbackHandler) PutCloudSettings(c *gin.Context) {
	var req struct {
		Args []store.FlashbackCloudArgItem `json:"args"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	for _, item := range req.Args {
		if !store.IsFlashbackCloudArg(item.Key) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "不支持的参数 key: " + item.Key})
			return
		}
	}
	if err := h.Store.SaveFlashbackCloudSettings(req.Args); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": h.Store.GetFlashbackCloudSettings()})
}

func (h *FlashbackHandler) SubmitDML(c *gin.Context) {
	var req struct {
		Reviewer    string `json:"reviewer"`
		Description string `json:"description"`
	}
	_ = c.ShouldBindJSON(&req)
	issue, err := h.engine().SubmitDML(c.Request.Context(), c.Param("id"), req.Reviewer, middleware.Username(c), req.Description)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": issue})
}
