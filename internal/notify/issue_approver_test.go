package notify

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestCollectApproverUsernamesSplitsComma(t *testing.T) {
	issue := &store.Issue{
		IssueType:    store.IssueTypeDML,
		AssignedTo:   "a,b",
		NeedRollback: `{"dba_assigned_to":"c,a","security_assigned_to":"d"}`,
	}
	got := collectApproverUsernames(issue)
	want := []string{"a", "b", "c", "d"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}

	dql := &store.Issue{
		IssueType:    store.IssueTypeDQL,
		AssignedTo:   "A,a",
		NeedRollback: `{"dba_assigned_to":"c"}`,
	}
	got = collectApproverUsernames(dql)
	if len(got) != 1 || got[0] != "A" {
		t.Fatalf("dql got %v", got)
	}
}

func TestTicketLinkIsLocal(t *testing.T) {
	if !ticketLinkIsLocal("http://127.0.0.1:5174/#/ops/tickets/1") {
		t.Fatal("127.0.0.1 should be local")
	}
	if !ticketLinkIsLocal("http://localhost:8800/#/ops/tickets/1") {
		t.Fatal("localhost should be local")
	}
	if ticketLinkIsLocal("https://dba.example.com/#/ops/tickets/1") {
		t.Fatal("production host should not be local")
	}
}
