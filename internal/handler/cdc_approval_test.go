package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestCDCNeedsApproval(t *testing.T) {
	if !cdcNeedsApproval("init") || !cdcNeedsApproval("add_table") || !cdcNeedsApproval("alter_schema") || !cdcNeedsApproval("remove_table") || !cdcNeedsApproval("delete_task") {
		t.Fatal("init/add_table/alter_schema/remove_table/delete_task must require approval")
	}
}

func TestCDCApproved(t *testing.T) {
	if cdcApproved(&store.CDCTask{ApprovalStatus: store.StatusPending, IssueID: "i1"}) {
		t.Fatal("pending must block")
	}
	if !cdcApproved(&store.CDCTask{ApprovalStatus: store.StatusApproved, IssueID: "i1"}) {
		t.Fatal("approved should run")
	}
	if !cdcApproved(&store.CDCTask{ApprovalStatus: "none"}) {
		t.Fatal("legacy none without issue should run")
	}
}

func TestCDCResolveReviewer(t *testing.T) {
	if got := cdcResolveReviewer("", " bob,carol ", "x"); got != "bob" {
		t.Fatalf("got %q", got)
	}
}
