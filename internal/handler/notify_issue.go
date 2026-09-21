package handler

import (
	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

// NotifyIssueCreatedAsync 工单创建成功后异步通知审批人（失败不影响创建结果）。
func NotifyIssueCreatedAsync(db *store.DB, encKey string, issue *store.Issue) {
	if db == nil || issue == nil {
		return
	}
	// 拷贝指针字段足够只读；异步避免拖慢接口
	go notify.IssueCreated(db, encKey, issue)
}
