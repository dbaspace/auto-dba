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
	go notify.IssueCreated(db, encKey, issue)
}

// NotifyIssueAssigneeChangedAsync 审核人转移后异步通知新人。
func NotifyIssueAssigneeChangedAsync(db *store.DB, encKey string, issue *store.Issue) {
	if db == nil || issue == nil {
		return
	}
	cp := *issue
	go notify.IssueAssigneeChanged(db, encKey, &cp)
}

// NotifyIssueExecFailedAsync 执行失败后异步通知提交人。
func NotifyIssueExecFailedAsync(db *store.DB, encKey string, issue *store.Issue, errMsg string) {
	if db == nil || issue == nil {
		return
	}
	cp := *issue
	msg := errMsg
	go notify.IssueExecFailed(db, encKey, &cp, msg)
}

// NotifyIssueAwaitExecAsync 审批后仍待执行时异步通知提交人。
func NotifyIssueAwaitExecAsync(db *store.DB, encKey string, issue *store.Issue) {
	if db == nil || issue == nil {
		return
	}
	cp := *issue
	reason := notify.AwaitExecNotifyReason(&cp)
	go notify.IssueAwaitExec(db, encKey, &cp, reason)
}

// NotifyIssueAwaitExecStuckAsync 待执行超时告警提交人。
func NotifyIssueAwaitExecStuckAsync(db *store.DB, encKey string, issue *store.Issue) {
	if db == nil || issue == nil {
		return
	}
	cp := *issue
	go notify.IssueAwaitExecStuck(db, encKey, &cp)
}
