package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/config"
	"auto-dba/internal/mail"
	"auto-dba/internal/middleware"
	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

type NotifyHandler struct {
	Store *store.DB
	Cfg   *config.Config
}

func (h *NotifyHandler) encKey() string {
	if h.Cfg == nil {
		return ""
	}
	return h.Cfg.FieldCrypto()
}

func (h *NotifyHandler) requireAdmin(c *gin.Context) bool {
	u, err := h.Store.GetUserByUsername(middleware.Username(c))
	if err != nil || u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return false
	}
	code := strings.ToLower(u.Role)
	if u.RoleID != "" {
		if r, _ := h.Store.GetRole(u.RoleID); r != nil {
			code = strings.ToLower(r.Code)
		}
	}
	if code != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"message": "仅管理员可配置认证"})
		return false
	}
	return true
}

func (h *NotifyHandler) Get(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	cfg, err := h.Store.GetNotifyConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	data := cfg.Public()
	data["providers"] = notify.ProviderStatus()
	// SMTP 状态（邮件区块徽章）：ready=已启用可自动发；configured=参数已齐可测发
	data["smtp_ready"] = false
	data["smtp_configured"] = false
	data["smtp_enabled"] = false
	if smtp, err := h.Store.GetSMTPConfig(h.encKey()); err == nil && smtp != nil {
		data["smtp_enabled"] = smtp.Enabled
		data["smtp_configured"] = smtp.Configured()
		data["smtp_ready"] = smtp.Ready()
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (h *NotifyHandler) Update(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req store.NotifyConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	existing, _ := h.Store.GetNotifyConfig()
	if existing == nil {
		existing = store.DefaultNotifyConfig()
	}
	// 页面不再编辑密钥/URL：保留库内旧值作 Nacos 未配时的回退
	req.WecomCorpID = existing.WecomCorpID
	req.WecomAgentID = existing.WecomAgentID
	req.WecomSecret = existing.WecomSecret
	req.WebhookURL = existing.WebhookURL
	req.PhoneAPIURL = existing.PhoneAPIURL
	// wecom_enabled / phone_enabled / webhook_enabled：由事件开关推导，避免页面丢字段
	if req.WecomOnAlert || req.WecomOnTicketCreate {
		req.WecomEnabled = true
	} else {
		req.WecomEnabled = existing.WecomEnabled
	}
	if req.PhoneOnAlert {
		req.PhoneEnabled = true
	} else {
		req.PhoneEnabled = existing.PhoneEnabled
	}
	if req.WebhookOnAlert || req.DingtalkOnAlert || req.RobotLarkOnAlert || req.RobotsOnTicketCreate {
		req.WebhookEnabled = true
	} else {
		req.WebhookEnabled = existing.WebhookEnabled
	}
	req.RobotRoutes = store.SanitizeRobotRoutes(req.RobotRoutes)
	if err := h.Store.SetNotifyConfig(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type notifyTestReq struct {
	Channel string `json:"channel"`
	To      string `json:"to"`
	For     string `json:"for"`   // alert | ticket（群机器人测试方向）
	Index   *int   `json:"index"` // Nacos robots 下标；可选
}

// Test POST /settings/notify/test — 按渠道发一条测试消息。
func (h *NotifyHandler) Test(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req notifyTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	ch := strings.ToLower(strings.TrimSpace(req.Channel))
	to := strings.TrimSpace(req.To)
	admin, _ := h.Store.GetUserByUsername(middleware.Username(c))
	text := fmt.Sprintf("[KeePlus] 通知渠道测试 channel=%s at %s", ch, time.Now().Format("2006-01-02 15:04:05"))
	md := "**KeePlus 通知测试**\n\n" + text

	switch ch {
	case "email":
		if to == "" && admin != nil {
			to = strings.TrimSpace(admin.Email)
		}
		smtpCfg, err := h.Store.GetSMTPConfig(h.encKey())
		if err != nil || smtpCfg == nil || !smtpCfg.Configured() {
			c.JSON(http.StatusBadRequest, gin.H{"message": "SMTP 未配置完整，请先在「邮件」Tab 填写服务器、发件人与密码"})
			return
		}
		if to == "" {
			to = strings.TrimSpace(smtpCfg.From)
		}
		if to == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请填写收件邮箱，或在个人资料绑定邮箱"})
			return
		}
		if err := mail.Send(mailConfig(smtpCfg), to, "[KeePlus] 通知渠道测试（邮件）", text, "text/plain; charset=UTF-8"); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "测试邮件已发送至 " + to})
		return

	case "lark":
		if to == "" && admin != nil {
			to = notify.ResolveLarkUserID(admin.IMType, admin.IMAccount)
		}
		if to == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请填写飞书 user_id，或在个人资料设置 im_type=lark 与 im_account"})
			return
		}
		if err := notify.SendViaErr(notify.ChannelLark, notify.Message{
			Title: "通知测试", Text: text, Markdown: md, LarkUserIDs: []string{to},
		}); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "飞书私聊测试已发送至 " + to})
		return

	case "lark_alert":
		if to == "" && admin != nil {
			to = notify.ResolveLarkUserID(admin.IMType, admin.IMAccount)
		}
		if to == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请填写飞书 user_id"})
			return
		}
		if err := notify.SendViaErr(notify.ChannelLarkAlert, notify.Message{
			Title: "告警应用测试", Text: text, Markdown: md, LarkUserIDs: []string{to},
		}); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "飞书告警应用测试已发送至 " + to})
		return

	case "wecom":
		toUser, toParty := notify.ParseWecomTestTarget(to)
		if toUser == "" && toParty == "" && admin != nil {
			toUser = notify.ResolveWecomUserID(admin.IMType, admin.IMAccount)
		}
		if toUser == "" && toParty == "" {
			// 测试未填时可用 Nacos 告警部门
			toParty = notify.NotifyModule().Provider.Wechat.PartyIDs.JoinWecomParty()
		}
		if toUser == "" && toParty == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请填写企微账号（如 ccj）或部门 ID（如 15 / party:15）"})
			return
		}
		ncfg, _ := h.Store.GetNotifyConfig()
		msg := notify.Message{Title: "通知测试", Text: text, Markdown: md, WecomToUser: toUser, WecomToParty: toParty}
		if notify.NotifyModule().WechatConfigured() {
			if err := notify.SendViaErr(notify.ChannelWecom, msg); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
				return
			}
		} else if ncfg != nil && ncfg.WecomCorpID != "" && ncfg.WecomSecret != "" {
			if err := notify.SendWecomMarkdownForTest(ncfg.WecomCorpID, ncfg.WecomSecret, ncfg.WecomAgentID, toUser, toParty, md); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
				return
			}
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"message": "企微未在 Nacos notify.provider.wechat 配置"})
			return
		}
		dest := toUser
		if toParty != "" {
			if dest != "" {
				dest += " / "
			}
			dest += "party:" + toParty
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "企微测试已发送至 " + dest})
		return

	case "sms":
		if to == "" && admin != nil {
			to = strings.TrimSpace(admin.Phone)
		}
		if to == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请填写手机号，或在个人资料绑定手机"})
			return
		}
		if err := notify.SendViaErr(notify.ChannelSMS, notify.Message{
			Title: "通知测试", Text: text, Phones: []string{to},
		}); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "短信测试已发送至 " + to})
		return

	case "call", "phone":
		if to == "" && admin != nil {
			to = strings.TrimSpace(admin.Phone)
		}
		if to == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "请填写手机号，或在个人资料绑定手机"})
			return
		}
		if err := notify.SendViaErr(notify.ChannelCall, notify.Message{
			Title: "通知测试", Text: text, Phones: []string{to},
		}); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": "电话测试已发起至 " + to})
		return

	case "dingtalk", "robot_lark", "lark_robot", "webhook", "robot_wechat", "robot":
		ncfg, _ := h.Store.GetNotifyConfig()
		robotFor := store.NormalizeRobotFor(req.For)
		if strings.TrimSpace(req.For) == "" {
			robotFor = "alert"
		}
		msg := notify.Message{
			Title: "通知测试", Text: text, Markdown: md,
			RobotFor: robotFor, RobotIndex: req.Index,
		}
		if ncfg != nil {
			msg.RobotRoutes = ncfg.RobotRoutes
		}
		switch ch {
		case "dingtalk":
			msg.RobotKind = "dingtalk"
		case "robot_lark", "lark_robot":
			msg.RobotKind = "lark"
		case "robot_wechat":
			msg.RobotKind = "wechat"
		case "webhook":
			if ncfg != nil && strings.TrimSpace(ncfg.WebhookURL) != "" {
				msg.RobotURLs = []string{ncfg.WebhookURL}
			}
		}
		if err := notify.SendViaErr(notify.ChannelRobot, msg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "message": fmt.Sprintf("群机器人测试已发送（for=%s）", robotFor)})
		return

	default:
		c.JSON(http.StatusBadRequest, gin.H{"message": "未知 channel，支持: email|lark|lark_alert|wecom|sms|call|dingtalk|robot_lark|webhook|robot"})
	}
}
