package notify

import (
	"fmt"
	"log"
	"strings"

	"auto-dba/internal/store"
)

// BackupFailed 备份失败通知策略创建人（若开启 email_on_backup_failed）。
func BackupFailed(db *store.DB, encKey string, policy *store.BackupPolicy, run *store.BackupRun) {
	if db == nil || policy == nil || run == nil {
		return
	}
	ncfg, err := db.GetNotifyConfig()
	if err != nil || ncfg == nil || !ncfg.EmailOnBackupFailed {
		return
	}
	smtpCfg, mailCfg, ok := loadSMTP(db, encKey, run.ID, "backup failed")
	if !ok {
		return
	}
	who := strings.TrimSpace(policy.CreatedBy)
	if who == "" {
		who = strings.TrimSpace(run.CreatedBy)
	}
	if who == "" || who == "scheduler" {
		log.Printf("notify backup failed: no recipient policy=%s run=%s", policy.ID, run.ID)
		return
	}
	base := "http://127.0.0.1:5174"
	if smtpCfg != nil {
		if b := strings.TrimRight(strings.TrimSpace(smtpCfg.FrontendBaseURL), "/"); b != "" {
			base = b
		}
	}
	link := fmt.Sprintf("%s/#/ops/backup/runs", base)
	msg := strings.TrimSpace(run.ErrorMessage)
	if msg == "" {
		msg = "备份失败，请打开执行记录查看详情"
	}
	subject := fmt.Sprintf("[auto-dba] 备份失败 %s", policy.Name)
	body := fmt.Sprintf(
		"您好，\n\n备份策略执行失败：\n\n"+
			"策略：%s\n实例：%s\n引擎：%s\n触发：%s\n原因：%s\n\n"+
			"请打开：%s\n",
		policy.Name,
		policy.InstanceID,
		policy.Engine,
		run.Trigger,
		msg,
		link,
	)
	sendToUsernames(db, mailCfg, []string{who}, subject, body, run.ID, "backup failed")
}
