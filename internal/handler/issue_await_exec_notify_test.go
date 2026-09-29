package handler

import (
	"testing"
	"time"

	"auto-dba/internal/notify"
	"auto-dba/internal/store"
)

func TestAwaitExecNotifyReasonMatchesPackage(t *testing.T) {
	it := &store.Issue{IssueType: store.IssueTypeDDL}
	if notify.AwaitExecNotifyReason(it) != "审批已通过，等待执行" {
		t.Fatal(notify.AwaitExecNotifyReason(it))
	}
}

func TestNotifyStaleAwaitExecIssuesNilHandler(t *testing.T) {
	var h *TicketHandler
	n, err := h.notifyStaleAwaitExecIssues(t.Context(), time.Now())
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	h = &TicketHandler{}
	n, err = h.notifyStaleAwaitExecIssues(t.Context(), time.Now())
	if err != nil || n != 0 {
		t.Fatalf("empty n=%d err=%v", n, err)
	}
}
