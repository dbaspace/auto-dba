package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"auto-dba/internal/mail"
	"auto-dba/internal/store"
)

// IssueCreated 工单创建后通知审批人（邮件）。异步调用方应 go IssueCreated(...)。
func IssueCreated(db *store.DB, encKey string, issue *store.Issue) {
	if db == nil || issue == nil {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.EmailOnTicketCreate {
		return
	}
	smtpCfg, err := db.GetSMTPConfig(encKey)
	if err != nil || smtpCfg == nil || !smtpCfg.Ready() {
		log.Printf("notify issue created: smtp not ready ticket=%s err=%v", issue.TicketNo, err)
		return
	}

	recipients := collectApproverUsernames(issue)
	if len(recipients) == 0 {
		return
	}

	base := strings.TrimRight(strings.TrimSpace(smtpCfg.FrontendBaseURL), "/")
	if base == "" {
		base = "http://127.0.0.1:5174"
	}
	link := fmt.Sprintf("%s/#/ops/tickets/%s", base, issue.ID)
	subject := fmt.Sprintf("[auto-dba] 待审批工单 %s", issue.TicketNo)
	body := fmt.Sprintf(
		"您好，\n\n有新的工单待您审批：\n\n"+
			"工单号：%s\n类型：%s\n库/对象：%s\n提交人：%s\n说明：%s\n\n"+
			"请打开：%s\n",
		issue.TicketNo,
		issue.IssueType,
		issue.DBName,
		issue.Submitter,
		strings.TrimSpace(issue.Description),
		link,
	)

	mailCfg := mail.Config{
		Host: smtpCfg.Host, Port: smtpCfg.Port, Username: smtpCfg.Username,
		Password: smtpCfg.Password, From: smtpCfg.From, UseTLS: smtpCfg.UseTLS,
	}
	seen := map[string]bool{}
	for _, uname := range recipients {
		u, _ := db.GetUserByUsername(uname)
		if u == nil {
			log.Printf("notify issue created: approver %q not found ticket=%s", uname, issue.TicketNo)
			continue
		}
		email := strings.TrimSpace(u.Email)
		if email == "" || !strings.Contains(email, "@") {
			log.Printf("notify issue created: approver %q has no email ticket=%s", uname, issue.TicketNo)
			continue
		}
		key := strings.ToLower(email)
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := mail.Send(mailCfg, email, subject, body, "text/plain; charset=UTF-8"); err != nil {
			log.Printf("notify issue created mail to %s ticket=%s: %v", email, issue.TicketNo, err)
			continue
		}
		log.Printf("notify issue created mail sent to %s user=%s ticket=%s", email, uname, issue.TicketNo)
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
