package notify

import (
	"testing"
	"time"

	"auto-dba/internal/store"
)

func TestAwaitExecNotifyReason(t *testing.T) {
	if AwaitExecNotifyReason(nil) == "" {
		t.Fatal("nil should have default reason")
	}
	dql := &store.Issue{IssueType: store.IssueTypeDQL}
	if got := AwaitExecNotifyReason(dql); got != "审批已通过，请下载导出结果" {
		t.Fatalf("dql=%q", got)
	}
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	sched := &store.Issue{IssueType: store.IssueTypeDDL, ScheduledAt: future}
	if got := AwaitExecNotifyReason(sched); got != "已预约，到期后请关注或手动执行" {
		t.Fatalf("scheduled=%q", got)
	}
	mig := &store.Issue{IssueType: store.IssueTypeMigration}
	if got := AwaitExecNotifyReason(mig); got != "审批已通过，请在对应任务页关注执行进度" {
		t.Fatalf("migration=%q", got)
	}
}

func TestScheduledInFuture(t *testing.T) {
	if scheduledInFuture("") {
		t.Fatal("empty")
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if scheduledInFuture(past) {
		t.Fatal("past")
	}
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	if !scheduledInFuture(future) {
		t.Fatal("future")
	}
}

func TestNotifyConfigAwaitStuckHours(t *testing.T) {
	if (&store.NotifyConfig{}).AwaitStuckHours() != store.DefaultAwaitExecStuckHours {
		t.Fatal("default hours")
	}
	if (&store.NotifyConfig{AwaitExecStuckHours: 5}).AwaitStuckHours() != 5 {
		t.Fatal("custom hours")
	}
}
