package store

import "testing"

func TestMigrationApproved(t *testing.T) {
	if MigrationApproved(&MigrationTask{ApprovalStatus: StatusPending, IssueID: "i1"}) {
		t.Fatal("pending must block")
	}
	if MigrationApproved(&MigrationTask{ApprovalStatus: "draft", IssueID: "i1"}) {
		t.Fatal("draft must block")
	}
	if !MigrationApproved(&MigrationTask{ApprovalStatus: StatusApproved, IssueID: "i1"}) {
		t.Fatal("approved should run")
	}
	if !MigrationApproved(&MigrationTask{}) {
		t.Fatal("legacy task without issue should run")
	}
}
