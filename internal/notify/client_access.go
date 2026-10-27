package notify

import (
	"fmt"
	"log"
	"strings"

	"auto-dba/internal/store"
)

// ClientAccessSecurityNotice 机器通道短时大量拒绝时通知管理员（机器人 + 可选邮件）。
func ClientAccessSecurityNotice(db *store.DB, encKey, username, clientType, path, reason string) {
	if db == nil {
		return
	}
	username = strings.TrimSpace(username)
	title := "[auto-dba] 客户端访问异常拒绝"
	text := fmt.Sprintf("用户：%s\n客户端：%s\n路径：%s\n原因：%s\n请检查是否未授权 MCP/CLI 滥用。",
		username, clientType, path, reason)
	SendVia(ChannelRobot, Message{Title: title, Text: text, Markdown: text})
	admins := adminUsernames(db)
	if len(admins) == 0 {
		log.Printf("notify client-access: no admin")
		return
	}
	smtpCfg, mailCfg, ok := loadSMTP(db, encKey, "client-access", "security")
	if !ok {
		return
	}
	_ = smtpCfg
	sendToUsernames(db, mailCfg, admins, title, text+"\n", "client-access", "security")
}
