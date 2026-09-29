package store

import (
	"strings"
	"testing"
)

func TestParseApproverWhitelist(t *testing.T) {
	got := ParseApproverWhitelist("alice, bob\nCarol，alice")
	if len(got) != 3 {
		t.Fatalf("want 3 unique, got %v", got)
	}
	if !strings.EqualFold(got[0], "alice") || !strings.EqualFold(got[1], "bob") || !strings.EqualFold(got[2], "Carol") {
		t.Fatalf("got=%v", got)
	}
	if len(ParseApproverWhitelist("")) != 0 {
		t.Fatal("empty should be empty")
	}
}

func TestExpandApproverNames(t *testing.T) {
	got := ExpandApproverNames([]string{"admin,dba", "admin", "mayu", "dba"})
	if len(got) != 3 {
		t.Fatalf("want 3 after union, got %v", got)
	}
	joined := strings.Join(got, "|")
	if strings.Contains(joined, "admin,dba") {
		t.Fatalf("literal csv must not remain: %v", got)
	}
	seen := map[string]bool{}
	for _, n := range got {
		seen[strings.ToLower(n)] = true
	}
	for _, want := range []string{"admin", "dba", "mayu"} {
		if !seen[want] {
			t.Fatalf("missing %s in %v", want, got)
		}
	}
}

func TestAppendAssigneeTransferHistory(t *testing.T) {
	h1 := AppendAssigneeTransferHistory("", "a->b by=a at=t1")
	h2 := AppendAssigneeTransferHistory(h1, "b->c by=a at=t2")
	if !strings.Contains(h2, "a->b") || !strings.Contains(h2, "b->c") {
		t.Fatalf("history=%s", h2)
	}
}

func TestCanApproveIssue(t *testing.T) {
	it := &Issue{AssignedTo: "alice", Status: StatusPending}
	if !(&DB{}).CanApproveIssue(it, "alice", false) {
		t.Fatal("assignee should approve")
	}
	if (&DB{}).CanApproveIssue(it, "bob", false) {
		t.Fatal("non-assignee without whitelist/priv should fail")
	}
	if !(&DB{}).CanApproveIssue(it, "bob", true) {
		t.Fatal("privileged should approve")
	}
}

func TestFormatAssigneeTransferLine(t *testing.T) {
	s := FormatAssigneeTransferLine("a", "b", "a")
	if !strings.Contains(s, "a->b") || !strings.Contains(s, "by=a") {
		t.Fatalf("line=%s", s)
	}
}
