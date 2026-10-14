package handler

import (
	"os"
	"testing"

	"auto-dba/internal/store"
)

// 审核人仍须在名单中（与 canActOnTarget 放宽提交人无关）。
func TestReviewerMustBeInApproverList(t *testing.T) {
	approvers := []string{"admin", "chenjiachi"}
	if usernameInList("ldap_dev", approvers) {
		t.Fatal("non-approver reviewer must be rejected")
	}
	if !usernameInList("admin", approvers) {
		t.Fatal("admin should be allowed as reviewer")
	}
}

func testStore(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to a postgres DSN to run store constraint tests")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCanActOnTargetConstraints(t *testing.T) {
	db := testStore(t)
	h := &TicketHandler{Store: db}
	insts, err := db.ListInstances("", "")
	if err != nil || len(insts) == 0 {
		t.Fatalf("seed instance missing: %v", err)
	}
	id := insts[0].ID

	ok, deny, msg, err := h.canActOnTarget("", id, "appdb", "admin")
	if err != nil || !ok {
		t.Fatalf("admin should pass registered db: deny=%v msg=%s err=%v", deny, msg, err)
	}

	ok, deny, msg, err = h.canActOnTarget("", id, "missing_db", "admin")
	if err != nil || ok || deny != targetActDenyDBNotRegistered {
		t.Fatalf("missing db: ok=%v deny=%v msg=%s err=%v", ok, deny, msg, err)
	}

	// 提交人不必在审批人名单；someone_else 对已登记库应通过
	ok, deny, msg, err = h.canActOnTarget("", id, "appdb", "someone_else")
	if err != nil || !ok {
		t.Fatalf("non-approver submitter should pass: deny=%v msg=%s err=%v", deny, msg, err)
	}

	ok, deny, msg, err = h.canActOnTarget("", "not-exist", "", "admin")
	if err != nil || ok || deny != targetActDenyInstanceMissing {
		t.Fatalf("missing instance: ok=%v deny=%v msg=%s err=%v", ok, deny, msg, err)
	}
}
