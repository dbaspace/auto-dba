package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"auto-dba/internal/mail"
	"auto-dba/internal/store"
)

const ticketLinkLocalWarn = "工单链接仍是本机地址，请在邮件设置填写前端访问地址"

// sendMail 工单邮件发送。测试可替换，生产走 mail.Send。
var sendMail = mail.Send

// loadTicketSMTP 读取工单邮件用的 SMTP。测试可替换。
var loadTicketSMTP = loadSMTP

// loadTicketNotify 读取工单通知开关。测试可替换。
var loadTicketNotify = func(db *store.DB) (*store.NotifyConfig, bool, error) {
	if db == nil {
		return nil, false, fmt.Errorf("读取通知配置失败")
	}
	return db.LoadNotifyConfig()
}

// findNotifyUser 按用户名查收件人。测试可替换。
var findNotifyUser = func(db *store.DB, username string) (*store.User, error) {
	if db == nil {
		return nil, nil
	}
	return db.GetUserByUsernameFold(username)
}

// saveNotifyAudit 写入通知审计。测试可替换。
var saveNotifyAudit = func(db *store.DB, row *store.QueryAudit) error {
	if db == nil || row == nil {
		return nil
	}
	return db.CreateQueryAudit(row)
}

// IssueCreated 工单创建后通知审批人（邮件）。异步调用方应 go IssueCreated(...)。
func IssueCreated(db *store.DB, encKey string, issue *store.Issue) {
	notifyApproverPending(db, encKey, issue, false)
}

// IssueAssigneeChanged 审核人转移后仅通知新人。
func IssueAssigneeChanged(db *store.DB, encKey string, issue *store.Issue) {
	notifyApproverPending(db, encKey, issue, true)
}

func notifyApproverPending(db *store.DB, encKey string, issue *store.Issue, transferred bool) {
	if issue == nil {
		return
	}
	ncfg, parseFailed, err := loadTicketNotify(db)
	if err != nil || ncfg == nil {
		msg := "读取通知配置失败"
		if err != nil {
			msg = err.Error()
		}
		log.Printf("notify ticket pending: read config failed ticket=%s err=%v", issue.TicketNo, err)
		recordNotifyAudit(db, issue, nil, "", "notify:", "", false, msg, 0)
		return
	}
	if parseFailed {
		log.Printf("notify ticket pending: config parse failed ticket=%s", issue.TicketNo)
		recordNotifyAudit(db, issue, nil, "", "notify:", "", false, "通知配置解析失败，已按默认关闭", 0)
		return
	}
	wantEmail := ncfg.EmailOnTicketCreate
	wantLark := ncfg.LarkOnTicketCreate
	wantWecom := ncfg.WecomOnTicketCreate
	wantRobot := ncfg.RobotsOnTicketCreate
	if !wantEmail && !wantLark && !wantWecom && !wantRobot {
		recordNotifyAudit(db, issue, nil, "", "notify:", "", false, "工单创建通知开关均未开启", 0)
		return
	}
	recipients := collectApproverUsernames(issue)
	if len(recipients) == 0 && !wantRobot {
		log.Printf("notify ticket pending: no approver usernames ticket=%s", issue.TicketNo)
		recordNotifyAudit(db, issue, nil, "", "notify:", "", false, "没有审批人", 0)
		return
	}
	if len(recipients) == 0 {
		log.Printf("notify ticket pending: no approver usernames, person channels skipped ticket=%s", issue.TicketNo)
		recordNotifyAudit(db, issue, nil, "", "notify:", "", false, "没有审批人", 0)
	}
	var smtpCfg *store.SMTPConfig
	if db != nil {
		smtpCfg, _ = db.GetSMTPConfig(encKey)
	}
	link := ticketLink(smtpCfg, issue.ID)
	linkWarn := ""
	if ticketLinkIsLocal(link) {
		linkWarn = ticketLinkLocalWarn
		log.Printf("notify ticket pending: %s ticket=%s link=%s", linkWarn, issue.TicketNo, link)
	}
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
	statement := subject + "\n\n" + body
	audit := &notifyAuditCtx{issue: issue, statement: statement, linkWarn: linkWarn}
	if wantEmail && len(recipients) > 0 {
		go deliverApproverEmail(db, encKey, issue, recipients, subject, body, statement, linkWarn, audit)
	}
	if wantLark && len(recipients) > 0 {
		sendTicketLark(db, issue, recipients, subject, body, statement, linkWarn)
	}
	sendApproverChannels(db, ncfg, recipients, subject, body, link, issue.TicketNo)
}

// deliverApproverEmail 单独发送邮件。报错、卡住、未就绪或 panic 只记邮件审计，不阻塞其它渠道。
func deliverApproverEmail(db *store.DB, encKey string, issue *store.Issue, recipients []string, subject, body, statement, linkWarn string, audit *notifyAuditCtx) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("notify email panic ticket=%s: %v", issue.TicketNo, rec)
			recordNotifyAudit(db, issue, nil, "", "email:", statement, false, joinAuditMessage("邮件发送异常", linkWarn), 0)
		}
	}()
	if _, mailCfg, ok := loadTicketSMTP(db, encKey, issue.TicketNo, "issue assignee notify"); ok {
		sendToUsernames(db, mailCfg, recipients, subject, body, issue.TicketNo, "issue assignee notify", audit)
		return
	}
	for _, name := range recipients {
		recordNotifyAudit(db, issue, nil, name, "email:", statement, false, joinAuditMessage("SMTP 未就绪", linkWarn), 0)
	}
}

func sendApproverChannels(db *store.DB, ncfg *store.NotifyConfig, usernames []string, subject, body, link, ticketNo string) {
	if ncfg == nil {
		return
	}
	text := body
	md := fmt.Sprintf("**%s**\n\n%s\n\n[打开工单](%s)", subject, body, link)

	if ncfg.WecomOnTicketCreate {
		toParts := make([]string, 0)
		seen := map[string]bool{}
		for _, uname := range usernames {
			u, _ := findNotifyUser(db, strings.TrimSpace(uname))
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
	Failed      string // 非空表示该实例上建账号失败（不附带密码）
	Permissions []string
}

var permissionLabels = map[string]string{
	"r":      "读操作",
	"c":      "写操作",
	"u":      "更新操作",
	"d":      "删除操作",
	"ddl":    "结构变更",
	"export": "导出",
}

// PermissionLabels 权限代码转中文名称，未知代码原样保留。
func PermissionLabels(perms []string) string {
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		if l, ok := permissionLabels[strings.ToLower(strings.TrimSpace(p))]; ok {
			out = append(out, l)
		} else {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// DCLNotifyItem 权限申请执行结果（飞书私聊申请人）。
type DCLNotifyItem struct {
	Address     string
	DBNames     []string
	Username    string
	Password    string // 明文；Created=false 时忽略
	Created     bool   // 本次新建账号
	Permissions []string
}

// DCLDone 权限申请工单执行成功后飞书私聊申请人（新建账号带密码，已存在注明沿用）。
func DCLDone(db *store.DB, issue *store.Issue, item DCLNotifyItem) {
	if db == nil || issue == nil || strings.TrimSpace(item.Username) == "" {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.LarkOnTicketCreate {
		return
	}
	recips := CollectLarkRecipients(db, []string{issue.Submitter}, "dcl ticket="+issue.TicketNo)
	if len(recips) == 0 {
		return
	}
	lines := []string{
		"地址：" + item.Address,
		"数据库：" + strings.Join(item.DBNames, ","),
		"账号：" + item.Username,
	}
	if item.Created {
		lines = append(lines, "密码："+item.Password)
	} else {
		lines = append(lines, "密码：沿用原密码")
	}
	if len(item.Permissions) > 0 {
		lines = append(lines, "权限："+PermissionLabels(item.Permissions))
	}
	note := strings.TrimSpace(issue.Description)
	if item.Created {
		note = joinNote(note, "账号已新建并完成授权")
	} else {
		note = joinNote(note, "账号已存在，仅授权")
	}
	if note != "" {
		lines = append(lines, "说明："+note)
	}
	text := fmt.Sprintf("工单 %s 已执行完成\n\n%s", issue.TicketNo, strings.Join(lines, "\n"))
	if err := SendViaErr(ChannelLark, Message{Title: "权限申请完成", Text: text, LarkRecipients: recips}); err != nil {
		log.Printf("notify dcl done ticket=%s: %v", issue.TicketNo, err)
		return
	}
	log.Printf("notify dcl done: sent to submitter=%s ticket=%s", issue.Submitter, issue.TicketNo)
}

// CreateDBDone 建库工单执行成功后飞书私聊申请人。
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
			switch {
			case it.Failed != "":
				lines = append(lines, "账号创建失败："+it.Failed)
			case it.Reused:
				lines = append(lines, "密码：沿用原密码")
				note = joinNote(note, "账号已存在，仅授权新库")
			default:
				lines = append(lines, "密码："+it.Password)
			}
			if len(it.Permissions) > 0 && it.Failed == "" {
				lines = append(lines, "权限："+PermissionLabels(it.Permissions))
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
	sendToUsernames(db, mailCfg, []string{issue.Submitter}, subject, body, issue.TicketNo, "issue exec failed", nil)
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
	sendToUsernames(db, mailCfg, []string{issue.Submitter}, subject, body, issue.TicketNo, "issue await exec", nil)
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
	sendToUsernames(db, mailCfg, []string{issue.Submitter}, subject, body, issue.TicketNo, "issue await stuck", nil)
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

func ticketLinkIsLocal(link string) bool {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "127.0.0.1" || host == "localhost"
}

func sendToUsernames(db *store.DB, mailCfg mail.Config, usernames []string, subject, body, ticketNo, tag string, audit *notifyAuditCtx) {
	seen := map[string]bool{}
	for _, uname := range usernames {
		uname = strings.TrimSpace(uname)
		if uname == "" {
			continue
		}
		start := time.Now()
		u, err := findNotifyUser(db, uname)
		if err != nil || u == nil {
			log.Printf("notify %s: user %q not found ticket=%s", tag, uname, ticketNo)
			if audit != nil {
				msg := fmt.Sprintf("查不到用户 %s", uname)
				if err != nil {
					msg = err.Error()
				}
				audit.write(db, nil, uname, "email:", false, msg, time.Since(start))
			}
			continue
		}
		email := strings.TrimSpace(u.Email)
		if email == "" || !strings.Contains(email, "@") {
			log.Printf("notify %s: user %q has no email ticket=%s", tag, uname, ticketNo)
			if audit != nil {
				audit.write(db, u, u.Username, "email:", false, "没有邮箱", time.Since(start))
			}
			continue
		}
		key := strings.ToLower(email)
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := sendMail(mailCfg, email, subject, body, "text/plain; charset=UTF-8"); err != nil {
			log.Printf("notify %s mail to %s ticket=%s: %v", tag, email, ticketNo, err)
			if audit != nil {
				audit.write(db, u, u.Username, "email:"+email, false, err.Error(), time.Since(start))
			}
			continue
		}
		log.Printf("notify %s mail sent to %s user=%s ticket=%s", tag, email, uname, ticketNo)
		if audit != nil {
			audit.write(db, u, u.Username, "email:"+email, true, "", time.Since(start))
		}
	}
}

func collectApproverUsernames(issue *store.Issue) []string {
	if issue == nil {
		return nil
	}
	var out []string
	add := func(s string) {
		for _, name := range store.ExpandApproverNames([]string{s}) {
			dup := false
			for _, x := range out {
				if strings.EqualFold(x, name) {
					dup = true
					break
				}
			}
			if !dup {
				out = append(out, name)
			}
		}
	}
	// DQL 多级审批只通知当前关审批人（assigned_to）；推进后由 IssueAssigneeChanged 再通知下一关。
	if issue.IssueType == store.IssueTypeDQL {
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

type notifyAuditCtx struct {
	issue     *store.Issue
	statement string
	linkWarn  string
}

func (a *notifyAuditCtx) write(db *store.DB, user *store.User, username, target string, ok bool, message string, elapsed time.Duration) {
	if a == nil {
		return
	}
	recordNotifyAudit(db, a.issue, user, username, target, a.statement, ok, joinAuditMessage(message, a.linkWarn), elapsed)
}

func joinAuditMessage(base, extra string) string {
	base = strings.TrimSpace(base)
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	if base == "" {
		return extra
	}
	return base + "；" + extra
}

func recordNotifyAudit(db *store.DB, issue *store.Issue, user *store.User, username, target, statement string, ok bool, message string, elapsed time.Duration) {
	if issue == nil {
		return
	}
	row := &store.QueryAudit{
		Kind:         "notify",
		ClientType:   "notify",
		Username:     strings.TrimSpace(username),
		TenantID:     issue.TenantID,
		InstanceName: issue.TicketNo,
		DBType:       issue.IssueType,
		Target:       target,
		Statement:    statement,
		Success:      ok,
		Message:      message,
		ElapsedMS:    elapsed.Milliseconds(),
	}
	if user != nil {
		row.UserID = user.ID
		if name := strings.TrimSpace(user.Username); name != "" {
			row.Username = name
		}
		row.DisplayName = user.DisplayName
	}
	if err := saveNotifyAudit(db, row); err != nil {
		who := row.Username
		if who == "" {
			who = strings.TrimSpace(issue.AssignedTo)
		}
		log.Printf("notify audit ticket=%s approver=%s: %v", issue.TicketNo, who, err)
	}
}

func sendTicketLark(db *store.DB, issue *store.Issue, usernames []string, subject, text, statement, linkWarn string) {
	seen := map[string]bool{}
	for _, uname := range usernames {
		uname = strings.TrimSpace(uname)
		key := strings.ToLower(uname)
		if uname == "" || seen[key] {
			continue
		}
		seen[key] = true
		start := time.Now()
		u, err := findNotifyUser(db, uname)
		if err != nil || u == nil {
			msg := fmt.Sprintf("查不到用户 %s", uname)
			if err != nil {
				msg = err.Error()
			}
			log.Printf("notify lark ticket=%s: %s", issue.TicketNo, msg)
			recordNotifyAudit(db, issue, nil, uname, "lark:", statement, false, joinAuditMessage(msg, linkWarn), time.Since(start))
			continue
		}
		cands := ResolveLarkRecipient(u)
		if len(cands) == 0 {
			log.Printf("notify lark ticket=%s: user %q has no im_account/email/phone", issue.TicketNo, u.Username)
			recordNotifyAudit(db, issue, u, u.Username, "lark:", statement, false, joinAuditMessage("无飞书身份", linkWarn), time.Since(start))
			continue
		}
		used, sendErr := SendLarkPerson(subject, text, cands)
		target := "lark:" + used
		if used == "" {
			target = "lark:" + strings.Join(cands, "|")
		}
		if sendErr != nil {
			log.Printf("notify lark ticket=%s user=%s: %v", issue.TicketNo, u.Username, sendErr)
			recordNotifyAudit(db, issue, u, u.Username, target, statement, false, joinAuditMessage(sendErr.Error(), linkWarn), time.Since(start))
			continue
		}
		recordNotifyAudit(db, issue, u, u.Username, target, statement, true, joinAuditMessage("", linkWarn), time.Since(start))
	}
}
