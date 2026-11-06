package handler

import (
	"strings"
	"testing"

	"auto-dba/internal/store"
)

func TestMergeCreateDBApproverNames(t *testing.T) {
	got := mergeCreateDBApproverNames("mayu, admin", "liuqi\nabc", []string{"dba_user", "mayu"})
	want := []string{"mayu", "admin", "liuqi", "abc", "dba_user"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, outsider := range []string{"caonengwei", "resto-ai-test"} {
		if _, ok := matchCreateDBApprover(got, outsider); ok {
			t.Fatalf("outsider %s included", outsider)
		}
	}
}

func TestKeepCreateDBApprover(t *testing.T) {
	if !keepCreateDBApprover("admin", nil) {
		t.Fatal("missing admin should stay")
	}
	if keepCreateDBApprover("abc", nil) {
		t.Fatal("missing user should drop")
	}
	if keepCreateDBApprover("admin", &store.User{LoginDisabled: true}) {
		t.Fatal("disabled admin should drop")
	}
	if !keepCreateDBApprover("mayu", &store.User{Username: "mayu"}) {
		t.Fatal("active user should stay")
	}
}

func TestResolveCreateDBApproverRejectsOutsider(t *testing.T) {
	if _, err := resolveCreateDBApprover(nil, ""); err == nil || !strings.Contains(err.Error(), "请选择审批人") {
		t.Fatalf("empty: %v", err)
	}
	_, err := resolveCreateDBApprover(nil, "abc")
	if err == nil || err.Error() != createDBApproverDenied {
		t.Fatalf("outsider: %v", err)
	}
}

func TestCreateDBTransferRejectsOutsider(t *testing.T) {
	h := &TicketHandler{}
	it := &store.Issue{IssueType: store.IssueTypeCreateDB, Submitter: "alice", TenantID: "biz"}
	if msg := h.validateTransferAssignee(it, "abc"); msg != createDBApproverDenied {
		t.Fatalf("transfer: %q", msg)
	}
	if msg := h.validateTransferAssignee(it, "alice"); msg != "不能将审核人转给提交人本人" {
		t.Fatalf("self: %q", msg)
	}
}
