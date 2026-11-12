package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func callReopen(h *TicketHandler, issueID string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/tickets/"+issueID+"/reopen", nil)
	c.Params = gin.Params{{Key: "id", Value: issueID}}
	c.Set(middleware.CtxTenantID, store.DefaultTenantID)
	c.Set(middleware.CtxUsername, "alice")
	h.Reopen(c)
	return w
}

func TestReopenFailedReturnsPendingWithoutRunning(t *testing.T) {
	db := testStore(t)
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		TenantID:   store.DefaultTenantID,
		IssueType:  store.IssueTypeDML,
		Status:     store.StatusFailed,
		Submitter:  "alice",
		AssignedTo: "bob",
		DBName:     "appdb",
		SQLContent: "UPDATE t SET a=1",
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })
	const result = `{"success":false,"message":"执行第 3 条 SQL 失败"}`
	if err := db.UpdateIssueExec(it.ID, store.StatusFailed, result); err != nil {
		t.Fatal(err)
	}

	w := callReopen(h, it.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	cur, err := db.GetIssue(it.ID)
	if err != nil || cur == nil {
		t.Fatal(err)
	}
	if cur.Status != store.StatusPending {
		t.Fatalf("status=%s want pending", cur.Status)
	}
	if cur.Status == store.StatusRunning {
		t.Fatal("reopen must not start execution")
	}
	if !strings.Contains(cur.ExecuteResult, "执行第 3 条 SQL 失败") {
		t.Fatalf("execute_result cleared: %s", cur.ExecuteResult)
	}
}

func TestReopenApprovedRejected(t *testing.T) {
	db := testStore(t)
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		TenantID:   store.DefaultTenantID,
		IssueType:  store.IssueTypeDML,
		Status:     store.StatusApproved,
		Submitter:  "alice",
		AssignedTo: "bob",
		DBName:     "appdb",
		SQLContent: "UPDATE t SET a=1",
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	w := callReopen(h, it.ID)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "仅执行失败或已终止的工单可退回待审批") {
		t.Fatalf("body=%s", w.Body.String())
	}
	cur, err := db.GetIssue(it.ID)
	if err != nil || cur == nil {
		t.Fatal(err)
	}
	if cur.Status != store.StatusApproved {
		t.Fatalf("status=%s want approved", cur.Status)
	}
}
