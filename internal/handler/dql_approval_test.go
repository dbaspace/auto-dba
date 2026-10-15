package handler

import (
	"testing"

	"auto-dba/internal/store"
)

func TestAdvanceDQLApproval_ThreeSteps(t *testing.T) {
	extra, err := buildDQLSubmitExtra(&ticketSubmitReq{
		NeedSensitive: true,
		DBAAssigned:   "dba1",
		SecAssigned:   "sec1",
		RowCount:      1000,
	}, "owner1")
	if err != nil {
		t.Fatal(err)
	}
	it := &store.Issue{
		IssueType:    store.IssueTypeDQL,
		Status:       store.StatusPending,
		AssignedTo:   "owner1",
		NeedRollback: extra,
	}

	advanced, err := advanceDQLApproval(it, "owner1")
	if err != nil || !advanced {
		t.Fatalf("step1 advanced=%v err=%v", advanced, err)
	}
	if it.AssignedTo != "dba1" {
		t.Fatalf("want dba1 got %s", it.AssignedTo)
	}
	ex, _ := parseDQLApprovalExtra(it.NeedRollback)
	if ex.Phase != dqlPhaseDBA || ex.AssigneeApprovedBy != "owner1" {
		t.Fatalf("phase=%s by=%s", ex.Phase, ex.AssigneeApprovedBy)
	}

	advanced, err = advanceDQLApproval(it, "dba1")
	if err != nil || !advanced {
		t.Fatalf("step2 advanced=%v err=%v", advanced, err)
	}
	if it.AssignedTo != "sec1" {
		t.Fatalf("want sec1 got %s", it.AssignedTo)
	}

	advanced, err = advanceDQLApproval(it, "sec1")
	if err != nil || advanced {
		t.Fatalf("step3 should finalize advanced=%v err=%v", advanced, err)
	}
	ex, _ = parseDQLApprovalExtra(it.NeedRollback)
	if ex.Phase != dqlPhaseDone || ex.SecApprovedBy != "sec1" {
		t.Fatalf("final phase=%s by=%s", ex.Phase, ex.SecApprovedBy)
	}
}

func TestAdvanceDQLApproval_SimpleNoExtraApprovers(t *testing.T) {
	extra, err := buildDQLSubmitExtra(&ticketSubmitReq{RowCount: 10}, "owner1")
	if err != nil {
		t.Fatal(err)
	}
	it := &store.Issue{
		IssueType:    store.IssueTypeDQL,
		Status:       store.StatusPending,
		AssignedTo:   "owner1",
		NeedRollback: extra,
	}
	advanced, err := advanceDQLApproval(it, "owner1")
	if err != nil || advanced {
		t.Fatalf("simple dql should finalize in one step advanced=%v err=%v", advanced, err)
	}
}

func TestAdvanceDQLApproval_LegacyInferPhase(t *testing.T) {
	it := &store.Issue{
		IssueType:  store.IssueTypeDQL,
		Status:     store.StatusPending,
		AssignedTo: "owner1",
		NeedRollback: `{"dba_assigned_to":"dba1","security_assigned_to":"sec1","row_count":600}`,
	}
	advanced, err := advanceDQLApproval(it, "owner1")
	if err != nil || !advanced {
		t.Fatalf("legacy step1 advanced=%v err=%v", advanced, err)
	}
	if it.AssignedTo != "dba1" {
		t.Fatalf("want dba1 got %s", it.AssignedTo)
	}
}

func TestMarkDQLRejected_KeepsAssigneePhase(t *testing.T) {
	extra, err := buildDQLSubmitExtra(&ticketSubmitReq{
		DBAAssigned: "dba1",
		SecAssigned: "sec1",
		RowCount:    1000,
	}, "owner1")
	if err != nil {
		t.Fatal(err)
	}
	it := &store.Issue{
		IssueType:    store.IssueTypeDQL,
		Status:       store.StatusPending,
		AssignedTo:   "owner1",
		NeedRollback: extra,
	}
	if err := markDQLRejected(it, "owner1"); err != nil {
		t.Fatal(err)
	}
	ex, _ := parseDQLApprovalExtra(it.NeedRollback)
	if ex.RejectedPhase != dqlPhaseAssignee || ex.RejectedBy != "owner1" {
		t.Fatalf("rejected_phase=%s by=%s", ex.RejectedPhase, ex.RejectedBy)
	}
	if ex.Phase != dqlPhaseAssignee {
		t.Fatalf("phase should stay assignee, got %s", ex.Phase)
	}
}
