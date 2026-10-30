package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

type SystemNoticeHandler struct {
	Store *store.DB
}

type systemNoticeSaveReq struct {
	Title                     string `json:"title"`
	Body                      string `json:"body"`
	Enabled                   bool   `json:"enabled"`
	ShowMode                  string `json:"show_mode"`
	BumpVersion               bool   `json:"bump_version"`
	ID                        string `json:"id"`
	TicketSubmitFreeze        bool   `json:"ticket_submit_freeze"`
	TicketSubmitFreezeMessage string `json:"ticket_submit_freeze_message"`
}

type systemNoticeAckReq struct {
	NoticeID string `json:"notice_id"`
	Version  int    `json:"version"`
}

// GetSettings GET /settings/system-notice — 管理端读取当前模板。
func (h *SystemNoticeHandler) GetSettings(c *gin.Context) {
	it, err := h.Store.GetSystemNoticeSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if it == nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{
			"id": "", "title": "", "body": "", "enabled": false,
			"show_mode": store.SystemNoticeModeOnVersionChange, "version": 1,
			"ticket_submit_freeze": false, "ticket_submit_freeze_message": "",
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": it})
}

// UpdateSettings PUT /settings/system-notice — 管理端保存模板（含发版冻结）。
func (h *SystemNoticeHandler) UpdateSettings(c *gin.Context) {
	var req systemNoticeSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "系统通知"
	}
	it := &store.SystemNotice{
		ID:                        strings.TrimSpace(req.ID),
		Title:                     title,
		Body:                      req.Body,
		Enabled:                   req.Enabled,
		ShowMode:                  req.ShowMode,
		TicketSubmitFreeze:        req.TicketSubmitFreeze,
		TicketSubmitFreezeMessage: req.TicketSubmitFreezeMessage,
	}
	if err := h.Store.UpsertSystemNotice(it, req.BumpVersion, middleware.Username(c)); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	saved, _ := h.Store.GetSystemNotice(it.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": saved})
}

// TicketSubmitFreeze GET /ticket-submit-freeze — 任意登录用户可读，供提交页预检。
func (h *SystemNoticeHandler) TicketSubmitFreeze(c *gin.Context) {
	enabled, msg, err := h.Store.GetTicketSubmitFreeze()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"enabled": enabled,
		"message": msg,
	}})
}

// Active GET /system-notice/active — 当前用户是否应看到登录弹框。
func (h *SystemNoticeHandler) Active(c *gin.Context) {
	n, err := h.Store.GetEnabledSystemNotice()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if n == nil || !n.Enabled {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"show": false}})
		return
	}
	uid := middleware.UserID(c)
	ackVer, hasAck, err := h.Store.GetSystemNoticeAck(uid, n.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	show := store.ShouldShowSystemNotice(n, ackVer, hasAck)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"show":      show,
		"notice_id": n.ID,
		"title":     n.Title,
		"body":      n.Body,
		"version":   n.Version,
		"show_mode": n.ShowMode,
	}})
}

// Ack POST /system-notice/ack — 用户确认弹框。
func (h *SystemNoticeHandler) Ack(c *gin.Context) {
	var req systemNoticeAckReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数无效"})
		return
	}
	uid := middleware.UserID(c)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}
	n, err := h.Store.GetSystemNotice(req.NoticeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if n == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "通知不存在"})
		return
	}
	ver := req.Version
	if ver < 1 {
		ver = n.Version
	}
	if err := h.Store.AckSystemNotice(uid, n.ID, ver); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
