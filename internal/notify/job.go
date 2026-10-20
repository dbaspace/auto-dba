package notify

import (
	"fmt"
	"log"
	"strings"

	"auto-dba/internal/store"
)

// JobFailed 平台定时任务失败通知（邮件 + 告警向机器人）。
func JobFailed(db *store.DB, encKey, jobKey, jobName, runID, errMsg string) {
	if db == nil {
		return
	}
	jobKey = strings.TrimSpace(jobKey)
	if jobName == "" {
		jobName = jobKey
	}
	msg := strings.TrimSpace(errMsg)
	if msg == "" {
		msg = "执行失败"
	}
	title := fmt.Sprintf("[auto-dba] 定时任务失败 %s", jobName)
	text := fmt.Sprintf("任务：%s (%s)\nRun：%s\n原因：%s\n请打开：配置中心 → 监控运维 → 定时任务", jobName, jobKey, runID, msg)

	// 群机器人（alert 方向）
	SendVia(ChannelRobot, Message{Title: title, Text: text, Markdown: text})

	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.EmailOnJobFailed {
		return
	}
	smtpCfg, mailCfg, ok := loadSMTP(db, encKey, runID, "job failed")
	if !ok {
		return
	}
	base := "http://127.0.0.1:5174"
	if smtpCfg != nil {
		if b := strings.TrimRight(strings.TrimSpace(smtpCfg.FrontendBaseURL), "/"); b != "" {
			base = b
		}
	}
	link := fmt.Sprintf("%s/#/config/scheduled-jobs", base)
	body := fmt.Sprintf("您好，\n\n平台定时任务执行失败：\n\n%s\n\n请打开：%s\n", text, link)
	admins := adminUsernames(db)
	if len(admins) == 0 {
		log.Printf("notify job failed: no admin recipient job=%s", jobKey)
		return
	}
	sendToUsernames(db, mailCfg, admins, title, body, runID, "job failed")
}

func adminUsernames(db *store.DB) []string {
	users, err := db.ListUsers()
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, u := range users {
		ok, _ := db.UserIsAdmin(u.ID)
		if !ok {
			continue
		}
		name := strings.TrimSpace(u.Username)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}
