package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"auto-dba/internal/middleware"
	"auto-dba/internal/store"
)

func ticketAssigneeTestStore(t *testing.T) *store.DB {
	t.Helper()
	dsn := os.Getenv("AUTO_DBA_TEST_DSN")
	if dsn == "" {
		t.Skip("set AUTO_DBA_TEST_DSN to run integration tests")
	}
	db, err := store.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func withUser(c *gin.Context, username, role string) {
	c.Set(middleware.CtxUsername, username)
	c.Set(middleware.CtxRole, role)
}

func TestTransferAssignee_SubmitterOK(t *testing.T) {
	db := ticketAssigneeTestStore(t)
	_ = db.SetGlobalArg(store.ArgIssueGlobalApproverWhitelist, "", "")
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		IssueType: store.IssueTypeDML, Status: store.StatusPending,
		Submitter: "alice", AssignedTo: "bob", DBName: "appdb", SQLContent: "SELECT 1",
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"assigned_to": "carol"})
	c.Request = httptest.NewRequest(http.MethodPut, "/tickets/"+it.ID+"/assignee", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: it.ID}}
	withUser(c, "alice", "developer")

	// resolveApprovers empty → no list constraint
	h.TransferAssignee(c)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	got, _ := db.GetIssue(it.ID)
	if got == nil || !strings.EqualFold(got.AssignedTo, "carol") {
		t.Fatalf("assigned_to=%v", got)
	}
	if !strings.Contains(got.AssigneeTransferHistory, "bob->carol") {
		t.Fatalf("history=%s", got.AssigneeTransferHistory)
	}
}

func TestTransferAssignee_NonSubmitterForbidden(t *testing.T) {
	db := ticketAssigneeTestStore(t)
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		IssueType: store.IssueTypeDML, Status: store.StatusPending,
		Submitter: "alice", AssignedTo: "bob", SQLContent: "SELECT 1",
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"assigned_to": "carol"})
	c.Request = httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: it.ID}}
	withUser(c, "bob", "developer")
	h.TransferAssignee(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d %s", w.Code, w.Body.String())
	}
}

func TestTransferAssignee_NonPending(t *testing.T) {
	db := ticketAssigneeTestStore(t)
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		IssueType: store.IssueTypeDML, Status: store.StatusApproved,
		Submitter: "alice", AssignedTo: "bob", SQLContent: "SELECT 1",
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"assigned_to": "carol"})
	c.Request = httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: it.ID}}
	withUser(c, "alice", "developer")
	h.TransferAssignee(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d %s", w.Code, w.Body.String())
	}
}

func runTransfer(t *testing.T, h *TicketHandler, issueID, user, to string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"assigned_to": to})
	c.Request = httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: issueID}}
	withUser(c, user, "developer")
	h.TransferAssignee(c)
	return w
}

func TestTransferAssignee_ToSubmitterRejected(t *testing.T) {
	db := ticketAssigneeTestStore(t)
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		IssueType: store.IssueTypeDML, Status: store.StatusPending,
		Submitter: "alice", AssignedTo: "bob", SQLContent: "SELECT 1",
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	w := runTransfer(t, h, it.ID, "alice", "Alice")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "提交人本人") {
		t.Fatalf("want 400 self-transfer, got %d %s", w.Code, w.Body.String())
	}
}

func TestTransferAssignee_MigrationWhitelist(t *testing.T) {
	db := ticketAssigneeTestStore(t)
	prev, _ := db.GetGlobalArg("migration_approver_user_ids")
	_ = db.SetGlobalArg("migration_approver_user_ids", "bob,carol", "")
	t.Cleanup(func() { _ = db.SetGlobalArg("migration_approver_user_ids", prev, "") })
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		IssueType: store.IssueTypeMigration, Status: store.StatusPending,
		Submitter: "alice", AssignedTo: "bob", SQLContent: "migration noop",
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	if w := runTransfer(t, h, it.ID, "alice", "dave"); w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 outside whitelist, got %d %s", w.Code, w.Body.String())
	}
	if w := runTransfer(t, h, it.ID, "alice", "carol"); w.Code != http.StatusOK {
		t.Fatalf("want 200 in whitelist, got %d %s", w.Code, w.Body.String())
	}
}

func TestApprove_Whitelist(t *testing.T) {
	db := ticketAssigneeTestStore(t)
	_ = db.SetGlobalArg(store.ArgIssueGlobalApproverWhitelist, "whitelist_user", "test")
	h := &TicketHandler{Store: db}
	it := &store.Issue{
		IssueType: store.IssueTypeKafka, Status: store.StatusPending,
		Submitter: "alice", AssignedTo: "bob", SQLContent: "kafka noop", Description: "wl-test",
	}
	if err := db.CreateIssue(it); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM tbl_issues WHERE id=?`, it.ID) })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/", nil)
	c.Params = gin.Params{{Key: "id", Value: it.ID}}
	withUser(c, "whitelist_user", "developer")
	h.Approve(c)
	if w.Code != http.StatusOK {
		t.Fatalf("whitelist approve want 200 got %d %s", w.Code, w.Body.String())
	}
	got, _ := db.GetIssue(it.ID)
	if got.Status != store.StatusApproved {
		t.Fatalf("status=%s", got.Status)
	}
	if !strings.EqualFold(got.AssignedTo, "bob") {
		t.Fatalf("assigned_to should stay bob, got %s", got.AssignedTo)
	}
}
