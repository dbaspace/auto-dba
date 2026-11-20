package handler

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/awr"
	"auto-dba/internal/config"
	"auto-dba/internal/slowreport"
	"auto-dba/internal/store"
)

type SlowReportHandler struct {
	Store *store.DB
	Cfg   *config.Config
	AWR   *awr.Service
}

func (h *SlowReportHandler) encKey() string {
	if h == nil || h.Cfg == nil {
		return ""
	}
	return h.Cfg.FieldCrypto()
}

func (h *SlowReportHandler) cpu() slowreport.CPUFunc {
	if h == nil || h.AWR == nil {
		return nil
	}
	return h.AWR.SlowReportCPUPeak
}

func (h *SlowReportHandler) Options(c *gin.Context) {
	envIDs := c.QueryArray("env_id")
	if len(envIDs) == 0 && strings.TrimSpace(c.Query("env_ids")) != "" {
		envIDs = strings.Split(c.Query("env_ids"), ",")
	}
	list, err := slowreport.Options(h.Store, envIDs, c.Query("tenant_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SlowReportHandler) List(c *gin.Context) {
	list, err := h.Store.ListSlowReports()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SlowReportHandler) Save(c *gin.Context) {
	var r store.SlowReport
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数不完整"})
		return
	}
	if id := strings.TrimSpace(c.Param("id")); id != "" {
		r.ID = id
	}
	if err := validateSlowReport(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if r.ID != "" {
		old, err := h.Store.GetSlowReport(r.ID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "规则不存在"})
			return
		}
		r.LastSentAt, r.LastFireKey, r.LastError, r.CreatedAt = old.LastSentAt, old.LastFireKey, old.LastError, old.CreatedAt
	}
	if err := h.Store.SaveSlowReport(&r); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": r})
}

func (h *SlowReportHandler) Delete(c *gin.Context) {
	if err := h.Store.DeleteSlowReport(c.Param("id")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SlowReportHandler) Preview(c *gin.Context) {
	var r store.SlowReport
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数不完整"})
		return
	}
	if len(r.EnvIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先选择环境"})
		return
	}
	scope := slowreport.Resolve(h.Store, r)
	if scope.Err != "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": scope.Err})
		return
	}
	doc, err := slowreport.Build(c.Request.Context(), h.Store, r, scope, time.Now(), h.cpu())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"html": slowreport.RenderHTML(doc)})
}

func (h *SlowReportHandler) Send(c *gin.Context) {
	rule, err := h.Store.GetSlowReport(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "规则不存在"})
		return
	}
	cfg, cfgErr := h.Store.GetSMTPConfig(h.encKey())
	if cfgErr != nil {
		cfg = nil
	}
	if err := slowreport.Deliver(c.Request.Context(), h.Store, *rule, "manual", "", time.Now(), h.cpu(), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	fresh, err := h.Store.GetSlowReport(rule.ID)
	if err == nil && fresh.LastError != "" {
		msg := fresh.LastError
		if cfgErr != nil && msg == "邮件服务未启用或未配置完整" {
			msg = cfgErr.Error()
		}
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": msg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *SlowReportHandler) Mails(c *gin.Context) {
	list, err := h.Store.ListSlowReportMails(c.Query("env"), 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

func (h *SlowReportHandler) Mail(c *gin.Context) {
	m, err := h.Store.GetSlowReportMail(c.Param("id"))
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"message": "周报不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": m})
}

func validateSlowReport(r *store.SlowReport) error {
	if len(r.EnvIDs) == 0 {
		return slowReportErr("请选择环境")
	}
	return nil
}

type slowReportErr string

func (e slowReportErr) Error() string { return string(e) }
