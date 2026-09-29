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
	if err != nil || ncfg == nil || !ncfg.EmailOnTicketCreate {
		return
	}
	smtpCfg, mailCfg, ok := loadSMTP(db, encKey, issue.TicketNo, "issue assignee notify")
	if !ok {
		return
	}
	recipients := collectApproverUsernames(issue)
	if len(recipients) == 0 {
		return
	}
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
	sendToUsernames(db, mailCfg, recipients, subject, body, issue.TicketNo, "issue assignee notify")
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
