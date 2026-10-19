package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"auto-dba/internal/mail"
	"auto-dba/internal/store"
)

// IssueCreated 工单创建后通知审批人（邮件）。异步调用方应 go IssueCreated(...)。
func IssueCreated(db *store.DB, encKey string, issue *store.Issue) {
	notifyApproverPending(db, encKey, issue, false)
}

// IssueAssigneeChanged 审核人转移后仅通知新人。
func IssueAssigneeChanged(db *store.DB, encKey string, issue *store.Issue) {
	notifyApproverPending(db, encKey, issue, true)
}

func notifyApproverPending(db *store.DB, encKey string, issue *store.Issue, transferred bool) {
	if db == nil || issue == nil {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil {
		return
	}
	wantEmail := ncfg.EmailOnTicketCreate
	wantLark := ncfg.LarkOnTicketCreate
	wantWecom := ncfg.WecomOnTicketCreate
	wantRobot := ncfg.RobotsOnTicketCreate
	if !wantEmail && !wantLark && !wantWecom && !wantRobot {
		return
	}
	recipients := collectApproverUsernames(issue)
	if len(recipients) == 0 && !wantRobot {
		log.Printf("notify ticket pending: no approver usernames ticket=%s", issue.TicketNo)
		return
	}
	if len(recipients) == 0 {
		log.Printf("notify ticket pending: no approver usernames, person channels skipped ticket=%s", issue.TicketNo)
	}
	smtpCfg, _ := db.GetSMTPConfig(encKey)
	link := ticketLink(smtpCfg, issue.ID)
	subject := fmt.Sprintf("[auto-dba] 待审批工单 %s", issue.TicketNo)
	intro := "有新的工单待您审批"
	if transferred {
		intro = "有工单的审核人已更换为您，请尽快审批"
		subject = fmt.Sprintf("[auto-dba] 审核人已更换 · 工单 %s", issue.TicketNo)
	}
	body := fmt.Sprintf(
		"您好，\n\n%s：\n\n"+
			"工单号：%s\n类型：%s\n库/对象：%s\n提交人：%s\n说明：%s\n\n"+
			"请打开：%s\n",
		intro,
		issue.TicketNo,
		issue.IssueType,
		issue.DBName,
		issue.Submitter,
		strings.TrimSpace(issue.Description),
		link,
	)
	if wantEmail && len(recipients) > 0 {
		if _, mailCfg, ok := loadSMTP(db, encKey, issue.TicketNo, "issue assignee notify"); ok {
			sendToUsernames(db, mailCfg, recipients, subject, body, issue.TicketNo, "issue assignee notify")
		}
	}
	sendApproverChannels(db, ncfg, recipients, subject, body, link, issue.TicketNo)
}

func sendApproverChannels(db *store.DB, ncfg *store.NotifyConfig, usernames []string, subject, body, link, ticketNo string) {
	if ncfg == nil {
		return
	}
	text := body
	md := fmt.Sprintf("**%s**\n\n%s\n\n[打开工单](%s)", subject, body, link)

	if ncfg.LarkOnTicketCreate {
		recips := CollectLarkRecipients(db, usernames, "ticket="+ticketNo)
		if len(recips) > 0 {
			SendVia(ChannelLark, Message{Title: subject, Text: text, Markdown: md, LarkRecipients: recips})
		}
	}

	if ncfg.WecomOnTicketCreate {
		toParts := make([]string, 0)
		seen := map[string]bool{}
		for _, uname := range usernames {
			u, _ := db.GetUserByUsername(strings.TrimSpace(uname))
			if u == nil {
				continue
			}
			id := ResolveWecomUserID(u.IMType, u.IMAccount)
			if id == "" {
				log.Printf("notify wecom: user %q has no wecom im_account ticket=%s", uname, ticketNo)
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			toParts = append(toParts, id)
		}
		to := strings.Join(toParts, "|")
		if to == "" {
			log.Printf("notify wecom: skip no wecom userid ticket=%s", ticketNo)
		} else {
			msg := Message{Title: subject, Text: text, Markdown: md, WecomToUser: to}
			// 优先 Nacos 企微；否则回退库表凭证
			if NotifyModule().WechatConfigured() {
				SendVia(ChannelWecom, msg)
			} else if ncfg.WecomEnabled && ncfg.WecomCorpID != "" && ncfg.WecomSecret != "" {
				if err := sendWecomMarkdown(ncfg.WecomCorpID, ncfg.WecomSecret, ncfg.WecomAgentID, to, "", md); err != nil {
					log.Printf("notify wecom ticket=%s: %v", ticketNo, err)
				}
			}
		}
	}

	if ncfg.RobotsOnTicketCreate {
		robots := NotifyModule().Robots
		matched := 0
		for i, r := range robots {
			if strings.TrimSpace(r.URL) == "" {
				continue
			}
			if robotMatchesFor(ncfg.RobotRouteFor(i), "ticket") {
				matched++
			}
		}
		if matched == 0 {
			log.Printf("notify robot ticket: skip — 无方向含「工单」的群机器人（请在通知页把条目改为工单/两者） ticket=%s routes=%v",
				ticketNo, ncfg.RobotRoutes)
		} else if err := SendViaErr(ChannelRobot, Message{
			Title: subject, Text: text, Markdown: md,
			RobotFor: "ticket", RobotRoutes: ncfg.RobotRoutes,
		}); err != nil {
			log.Printf("notify robot ticket=%s: %v", ticketNo, err)
		} else {
			log.Printf("notify robot ticket: sent to %d robot(s) ticket=%s", matched, ticketNo)
		}
	}
}

// CreateDBNotifyItem 建库结果中一个实例的信息。
type CreateDBNotifyItem struct {
	Address     string
	DBNames     []string
	Username    string
	Password    string // 明文；Reused=true 时忽略
	Reused      bool   // 账号已存在，沿用原密码
	Permissions []string
}

// CreateDBDone 建库工单执行成功后飞书私聊申请人（唯一给申请人发结果的工单类型）。
func CreateDBDone(db *store.DB, issue *store.Issue, items []CreateDBNotifyItem, warn string) {
	if db == nil || issue == nil || len(items) == 0 {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.LarkOnTicketCreate {
		return
	}
	recips := CollectLarkRecipients(db, []string{issue.Submitter}, "createdb ticket="+issue.TicketNo)
	if len(recips) == 0 {
		return
	}
	desc := strings.TrimSpace(issue.Description)
	var blocks []string
	for _, it := range items {
		lines := []string{
			"地址：" + it.Address,
			"数据库：" + strings.Join(it.DBNames, ","),
		}
		note := desc
		if strings.TrimSpace(it.Username) != "" {
			lines = append(lines, "账号："+it.Username)
			if it.Reused {
				lines = append(lines, "密码：沿用原密码")
				note = joinNote(note, "账号已存在，仅授权新库")
			} else {
				lines = append(lines, "密码："+it.Password)
			}
			if len(it.Permissions) > 0 {
				lines = append(lines, "权限："+strings.Join(it.Permissions, ","))
			}
		}
		if w := strings.TrimSpace(warn); w != "" {
			note = joinNote(note, "授权提示："+w)
		}
		if note != "" {
			lines = append(lines, "说明："+note)
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	text := fmt.Sprintf("工单 %s 已执行完成\n\n%s", issue.TicketNo, strings.Join(blocks, "\n\n"))
	if err := SendViaErr(ChannelLark, Message{Title: "建库工单完成", Text: text, LarkRecipients: recips}); err != nil {
		log.Printf("notify createdb done ticket=%s: %v", issue.TicketNo, err)
		return
	}
	log.Printf("notify createdb done: sent to submitter=%s ticket=%s", issue.Submitter, issue.TicketNo)
}

func joinNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "；" + b
}

// IssueExecFailed 工单执行失败通知提交人。
func IssueExecFailed(db *store.DB, encKey string, issue *store.Issue, errMsg string) {
	if db == nil || issue == nil {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.EmailOnTicketExecFailed {
		return
	}
	smtpCfg, mailCfg, ok := loadSMTP(db, encKey, issue.TicketNo, "issue exec failed")
	if !ok {
		return
	}
	link := ticketLink(smtpCfg, issue.ID)
	msg := strings.TrimSpace(errMsg)
	if msg == "" {
		msg = "执行失败，请打开工单查看详情"
	}
	subject := fmt.Sprintf("[auto-dba] 工单执行失败 %s", issue.TicketNo)
	body := fmt.Sprintf(
		"您好，\n\n您提交的工单执行失败：\n\n"+
			"工单号：%s\n类型：%s\n库/对象：%s\n状态：执行失败\n原因：%s\n\n"+
			"请打开：%s\n",
		issue.TicketNo,
		issue.IssueType,
		issue.DBName,
		msg,
		link,
	)
	sendToUsernames(db, mailCfg, []string{issue.Submitter}, subject, body, issue.TicketNo, "issue exec failed")
}

// IssueAwaitExec 审批通过后仍待执行时通知提交人。
func IssueAwaitExec(db *store.DB, encKey string, issue *store.Issue, reason string) {
	if db == nil || issue == nil {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.EmailOnTicketAwaitExec {
		return
	}
	smtpCfg, mailCfg, ok := loadSMTP(db, encKey, issue.TicketNo, "issue await exec")
	if !ok {
		return
	}
	link := ticketLink(smtpCfg, issue.ID)
	why := strings.TrimSpace(reason)
	if why == "" {
		why = "审批已通过，等待执行"
	}
	subject := fmt.Sprintf("[auto-dba] 工单待执行 %s", issue.TicketNo)
	body := fmt.Sprintf(
		"您好，\n\n您提交的工单已审批通过，当前待执行：\n\n"+
			"工单号：%s\n类型：%s\n库/对象：%s\n说明：%s\n\n"+
			"请打开：%s\n",
		issue.TicketNo,
		issue.IssueType,
		issue.DBName,
		why,
		link,
	)
	sendToUsernames(db, mailCfg, []string{issue.Submitter}, subject, body, issue.TicketNo, "issue await exec")
}

// IssueAwaitExecStuck 长时间停在待执行时告警提交人。
func IssueAwaitExecStuck(db *store.DB, encKey string, issue *store.Issue) {
	if db == nil || issue == nil {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.EmailOnTicketAwaitStuck {
		return
	}
	smtpCfg, mailCfg, ok := loadSMTP(db, encKey, issue.TicketNo, "issue await stuck")
	if !ok {
		return
	}
	link := ticketLink(smtpCfg, issue.ID)
	hours := ncfg.AwaitStuckHours()
	subject := fmt.Sprintf("[auto-dba] 工单长时间待执行 %s", issue.TicketNo)
	body := fmt.Sprintf(
		"您好，\n\n您提交的工单已超过 %d 小时仍处于待执行状态，请及时处理：\n\n"+
			"工单号：%s\n类型：%s\n库/对象：%s\n状态：已通过（待执行）\n\n"+
			"请打开：%s\n",
		hours,
		issue.TicketNo,
		issue.IssueType,
		issue.DBName,
		link,
	)
	sendToUsernames(db, mailCfg, []string{issue.Submitter}, subject, body, issue.TicketNo, "issue await stuck")
}

// AwaitExecNotifyReason 审批后待执行邮件的说明文案。
func AwaitExecNotifyReason(issue *store.Issue) string {
	if issue == nil {
		return "审批已通过，等待执行"
	}
	if scheduledInFuture(issue.ScheduledAt) {
		return "已预约，到期后请关注或手动执行"
	}
	switch strings.ToLower(strings.TrimSpace(issue.IssueType)) {
	case store.IssueTypeDQL:
		return "审批已通过，请下载导出结果"
	case store.IssueTypeArchive, store.IssueTypeFlashback, store.IssueTypeCDC, store.IssueTypeMigration:
		return "审批已通过，请在对应任务页关注执行进度"
	default:
		return "审批已通过，等待执行"
	}
}

func scheduledInFuture(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	now := time.Now()
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t.After(now)
		}
		if t, err := time.Parse(layout, raw); err == nil {
			return t.After(now)
		}
	}
	return false
}

func loadSMTP(db *store.DB, encKey, ticketNo, tag string) (*store.SMTPConfig, mail.Config, bool) {
	smtpCfg, err := db.GetSMTPConfig(encKey)
	if err != nil || smtpCfg == nil || !smtpCfg.Ready() {
		log.Printf("notify %s: smtp not ready ticket=%s err=%v", tag, ticketNo, err)
		return nil, mail.Config{}, false
	}
	return smtpCfg, mail.Config{
		Host: smtpCfg.Host, Port: smtpCfg.Port, Username: smtpCfg.Username,
		Password: smtpCfg.Password, From: smtpCfg.From, UseTLS: smtpCfg.UseTLS,
		AuthWay: smtpCfg.AuthWay, Rate: smtpCfg.Rate,
	}, true
}

func ticketLink(smtpCfg *store.SMTPConfig, issueID string) string {
	base := "http://127.0.0.1:5174"
	if smtpCfg != nil {
		if b := strings.TrimRight(strings.TrimSpace(smtpCfg.FrontendBaseURL), "/"); b != "" {
			base = b
		}
	}
	return fmt.Sprintf("%s/#/ops/tickets/%s", base, issueID)
}

func sendToUsernames(db *store.DB, mailCfg mail.Config, usernames []string, subject, body, ticketNo, tag string) {
	seen := map[string]bool{}
	for _, uname := range usernames {
		uname = strings.TrimSpace(uname)
		if uname == "" {
			continue
		}
		u, _ := db.GetUserByUsername(uname)
		if u == nil {
			log.Printf("notify %s: user %q not found ticket=%s", tag, uname, ticketNo)
			continue
		}
		email := strings.TrimSpace(u.Email)
		if email == "" || !strings.Contains(email, "@") {
			log.Printf("notify %s: user %q has no email ticket=%s", tag, uname, ticketNo)
			continue
		}
		key := strings.ToLower(email)
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := mail.Send(mailCfg, email, subject, body, "text/plain; charset=UTF-8"); err != nil {
			log.Printf("notify %s mail to %s ticket=%s: %v", tag, email, ticketNo, err)
			continue
		}
		log.Printf("notify %s mail sent to %s user=%s ticket=%s", tag, email, uname, ticketNo)
	}
}

func collectApproverUsernames(issue *store.Issue) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, x := range out {
			if strings.EqualFold(x, s) {
				return
			}
		}
		out = append(out, s)
	}
	// DQL 多级审批只通知当前关审批人（assigned_to）；推进后由 IssueAssigneeChanged 再通知下一关。
	if issue != nil && issue.IssueType == store.IssueTypeDQL {
		add(issue.AssignedTo)
		return out
	}
	add(issue.AssignedTo)
	if strings.TrimSpace(issue.NeedRollback) == "" {
		return out
	}
	var extra map[string]any
	if json.Unmarshal([]byte(issue.NeedRollback), &extra) != nil {
		return out
	}
	for _, k := range []string{"dba_assigned_to", "security_assigned_to"} {
		if v, ok := extra[k]; ok {
			add(fmt.Sprint(v))
		}
	}
	return out
}
