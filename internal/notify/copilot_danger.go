package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"auto-dba/internal/config"
	"auto-dba/internal/mail"
	"auto-dba/internal/store"
)

// copilotRobotChannels 已配置的群机器人。危险操作通知不依赖各渠道的告警总闸。
func copilotRobotChannels(robots []config.NotifyRobotConfig) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range robots {
		if strings.TrimSpace(r.URL) == "" && strings.TrimSpace(r.Kind) == "" {
			continue
		}
		var ch string
		switch normalizeRobotKind(r.Kind, r.URL) {
		case "lark":
			ch = "robot_lark"
		case "dingtalk":
			ch = "dingtalk"
		case "wechat":
			ch = "robot_wechat"
		}
		if ch == "" || seen[ch] {
			continue
		}
		seen[ch] = true
		out = append(out, ch)
	}
	return out
}

// CopilotDangerNotice 危险对话被拒绝后，按认证配置开关通知指定人和邮箱。
// 开关关闭时不外发。调用方仍须拒绝执行。
func CopilotDangerNotice(db *store.DB, encKey, user, tenant, sessionID, reason, summary string) {
	if db == nil {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.CopilotDangerAlert {
		log.Printf("copilot-danger: refused user=%s session=%s reason=%s notify=off", strings.TrimSpace(user), strings.TrimSpace(sessionID), reason)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	when := copilotDangerWhen(time.Now())
	sum := sha256.Sum256([]byte(strings.TrimSpace(user) + "|" + strings.TrimSpace(sessionID) + "|" + now + "|" + summary))
	fp := hex.EncodeToString(sum[:])
	a := &store.Alert{
		TenantID:       strings.TrimSpace(tenant),
		Fingerprint:    fp,
		Severity:       store.AlertSeverityP2,
		Title:          "AI助手危险操作",
		Service:        "copilot",
		Status:         store.AlertStatusFiring,
		Handle:         store.AlertHandleUnclaimed,
		Labels:         map[string]string{"user": strings.TrimSpace(user), "tenant": strings.TrimSpace(tenant), "reason": trimNote(reason, 120), "session": strings.TrimSpace(sessionID), "operated_at": when},
		Annotations:    map[string]string{"summary": trimNote(summary, 240)},
		StartedAt:      now,
		AggregateCount: 1,
	}
	if err := db.UpsertAlert(a); err != nil {
		log.Printf("copilot-danger: save alert: %v", err)
	} else {
		_ = db.AddAlertEvent(&store.AlertEvent{
			AlertID: a.ID, Action: "trigger", Operator: strings.TrimSpace(user),
			Detail: trimNote(reason+" "+summary, 240),
		})
	}
	p := AlertPayload{
		Event:          "trigger",
		AlertID:        a.ID,
		Severity:       "P2",
		Title:          "AI助手危险操作",
		Status:         "refused",
		Service:        "copilot",
		Handle:         "refused",
		ContextSummary: trimNote(summary, 240),
		Labels: map[string]string{
			"user":        strings.TrimSpace(user),
			"tenant":      strings.TrimSpace(tenant),
			"reason":      trimNote(reason, 120),
			"operated_at": when,
		},
		StartedAt: now,
		At:        now,
	}
	deliverCopilotDanger(db, encKey, ncfg, p)
}

func deliverCopilotDanger(db *store.DB, encKey string, ncfg *store.NotifyConfig, p AlertPayload) {
	users := ncfg.CopilotDangerUsers
	emails := ncfg.CopilotDangerEmails
	operator := ""
	when := ""
	reason := ""
	if p.Labels != nil {
		operator = p.Labels["user"]
		when = p.Labels["operated_at"]
		reason = p.Labels["reason"]
	}
	display := ""
	if u, _ := db.GetUserByUsername(strings.TrimSpace(operator)); u != nil {
		display = u.DisplayName
	}
	title, text, md := formatCopilotDangerMessage(operator, display, when, p.ContextSummary, reason)
	sendCopilotDangerMail(db, encKey, users, emails, title, text, p.AlertID)
	if NotifyModule().LarkConfigured() {
		if recips := CollectLarkRecipients(db, users, "copilot-danger"); len(recips) > 0 {
			SendVia(ChannelLark, Message{Title: title, Text: text, Markdown: md, LarkRecipients: recips})
		}
	}
	if NotifyModule().WechatConfigured() || ncfg.WecomEnabled {
		if ids := collectWecomIDs(db, users); len(ids) > 0 {
			SendVia(ChannelWecom, Message{Markdown: md, Text: text, WecomToUser: strings.Join(ids, "|")})
		}
	}
	robots := copilotRobotChannels(NotifyModule().Robots)
	if len(users) == 0 && len(emails) == 0 && len(robots) == 0 {
		log.Printf("copilot-danger: no recipient configured alert=%s", p.AlertID)
	}
	for _, ch := range robots {
		kind := "lark"
		switch ch {
		case "dingtalk":
			kind = "dingtalk"
		case "robot_wechat":
			kind = "wechat"
		}
		SendVia(ChannelRobot, robotAlertMessage(ncfg, title, text, md, kind))
	}
}

func copilotDangerWhen(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return t.In(loc).Format("2006-01-02 15:04:05")
}

func formatCopilotDangerMessage(operator, displayName, when, content, reason string) (title, text, md string) {
	who := strings.TrimSpace(operator)
	name := strings.TrimSpace(displayName)
	if name != "" && !strings.EqualFold(name, who) {
		if who == "" {
			who = name
		} else {
			who = name + "（" + who + "）"
		}
	}
	if who == "" {
		who = "未知"
	}
	if strings.TrimSpace(when) == "" {
		when = "未知"
	}
	content = strings.TrimSpace(content)
	if content == "" {
		content = "（空）"
	}
	title = "AI助手危险操作"
	text = fmt.Sprintf("操作人：%s\n操作时间：%s\n请求内容：%s", who, when, content)
	md = fmt.Sprintf("**操作人：** %s\n**操作时间：** %s\n**请求内容：** %s", who, when, content)
	if r := strings.TrimSpace(reason); r != "" {
		text += "\n原因：" + r
		md += "\n**原因：** " + r
	}
	return title, text, md
}

func sendCopilotDangerMail(db *store.DB, encKey string, users, emails []string, subject, body, alertID string) {
	if len(users) == 0 && len(emails) == 0 {
		return
	}
	_, mailCfg, ok := loadSMTP(db, encKey, alertID, "copilot-danger")
	if !ok {
		return
	}
	seen := map[string]bool{}
	var tos []string
	add := func(email string) {
		email = strings.TrimSpace(email)
		if !strings.Contains(email, "@") {
			return
		}
		key := strings.ToLower(email)
		if seen[key] {
			return
		}
		seen[key] = true
		tos = append(tos, email)
	}
	for _, email := range emails {
		add(email)
	}
	for _, name := range users {
		u, _ := db.GetUserByUsername(strings.TrimSpace(name))
		if u == nil {
			log.Printf("copilot-danger: user %q not found", name)
			continue
		}
		add(u.Email)
	}
	if len(tos) == 0 {
		log.Printf("copilot-danger: mail skip no address alert=%s", alertID)
		return
	}
	for _, to := range tos {
		if err := mail.Send(mailCfg, to, subject, body, "text/plain; charset=UTF-8"); err != nil {
			log.Printf("copilot-danger: mail to %s alert=%s: %v", to, alertID, err)
			continue
		}
		log.Printf("copilot-danger: mail sent to %s alert=%s", to, alertID)
	}
}

func trimNote(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
