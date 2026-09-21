package flashback

import (
	"testing"

	"auto-dba/internal/store"
)

func TestFlashbackApproved(t *testing.T) {
	if flashbackApproved(nil) {
		t.Fatal("nil should not be approved")
	}
	if flashbackApproved(&TaskRow{ApprovalStatus: store.StatusPending, IssueID: "i1"}) {
		t.Fatal("pending approval must block run")
	}
	if flashbackApproved(&TaskRow{ApprovalStatus: store.StatusRejected, IssueID: "i1"}) {
		t.Fatal("rejected must block run")
	}
	if !flashbackApproved(&TaskRow{ApprovalStatus: store.StatusApproved, IssueID: "i1"}) {
		t.Fatal("approved should run")
	}
	if !flashbackApproved(&TaskRow{}) {
		t.Fatal("legacy task without issue should still run")
	}
}

func TestFlashbackResolveReviewer(t *testing.T) {
	if got := flashbackResolveReviewer(&FlashbackTaskReq{Approver: "bob", Reviewer: "alice"}); got != "alice" {
		t.Fatalf("got %q", got)
	}
	if got := flashbackResolveReviewer(&FlashbackTaskReq{AssignedTo: "carol"}); got != "carol" {
		t.Fatalf("got %q", got)
	}
	if got := flashbackResolveReviewer(&FlashbackTaskReq{}); got != "" {
		t.Fatalf("empty req got %q", got)
	}
}
